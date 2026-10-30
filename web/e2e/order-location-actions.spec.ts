import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';
import type { Order, User } from '../src/lib/types';

const orders: Order[] = [0, 1].map((index) => ({
  id: `location-order-${index}`,
  user_id: 'owner',
  nickname: `捐赠者 ${index}`,
  avatar_url: '',
  amount_fen: 888,
  channel: 'alipay',
  status: 'paid',
  protocol: 'legacy',
  merchant_order_no: `legacy-${index}`,
  api_trade_no: '',
  trade_no: '',
  message: '属地刷新回归',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
  remote_close_status: '',
  created_at: '2026-10-30T08:00:00Z',
  expires_at: '2026-10-30T08:30:00Z',
  paid_at: '2026-10-30T08:01:00Z',
  ip: '192.0.2.123',
  province: '广东',
}));
const user: User = {
  id: 'owner',
  nickname: '捐赠者',
  avatar_url: '',
  status: 'active',
  admin: false,
  bindings: [],
  created_at: '2026-10-29T08:00:00Z',
  last_login_at: '2026-10-30T08:00:00Z',
  order_count: 2,
  paid_fen: 1776,
};

async function setup(page: Page, locationDisabled = false) {
  const previews: { action: string; ids: string[] }[] = [];
  let current = { action: '', ids: [] as string[] };
  let refreshed = false;
  let operationPolls = 0;
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'csrf',
        providers: [],
        version: { built_at: '', commit: '', tag: '', version: 'test' },
      };
    if (path.endsWith('/user/profile'))
      data = {
        ...user,
        admin: true,
        provider: 'oidc',
        csrf_token: 'csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/admin/overview'))
      data = { paid_fen: 1776, paid: 2, pending: 0, closed: 0, daily: [], channels: [] };
    if (path.endsWith('/admin/order/list')) data = { items: orders, total: 2, page: 1, page_size: 20 };
    if (path.endsWith('/admin/user/list')) data = { items: [user], total: 1, page: 1, page_size: 20 };
    if (path.endsWith('/admin/user/detail')) data = user;
    if (path.endsWith('/admin/order/detail'))
      data = {
        ...orders[0],
        location: {
          country: '中国',
          province: refreshed ? '浙江' : '广东',
          city: refreshed ? '杭州' : '广州',
          district: '',
          street: '',
          isp: '',
          asn: '',
          source: 'mmdb',
          version: 'test',
          queried_at: '2026-10-31T00:00:00Z',
        },
        jobs: refreshed
          ? [
              {
                id: 'geo-job',
                kind: 'geo',
                source: 'admin_refresh',
                status: 'done',
                result: 'located_mmdb',
                attempts: 1,
                created_at: '2026-10-31T00:00:00Z',
                run_at: '2026-10-31T00:00:00Z',
              },
            ]
          : [],
      };
    if (path.endsWith('/action/preview')) {
      current = route.request().postDataJSON();
      previews.push(current);
      data = {
        id: 'operation',
        action: current.action,
        expires_at: '2099-01-01T00:00:00Z',
        items: current.ids.map((id) => ({
          id,
          status: 'paid',
          result: locationDisabled ? 'disabled' : 'ready',
          amount_fen: 888,
        })),
      };
    }
    if (path.endsWith('/action/execute')) {
      refreshed = current.action === 'refresh_location';
      data = {
        id: 'operation',
        action: current.action,
        confirmed: true,
        items: current.ids.map((id) => ({ id, status: 'paid', result: 'queued', amount_fen: 888 })),
      };
    }
    if (path.endsWith('/operation/detail')) {
      operationPolls += 1;
      data = {
        id: 'operation',
        action: current.action,
        confirmed: true,
        items: current.ids.map((id) => ({
          id,
          status: operationPolls > 1 ? 'done' : 'ready',
          result: operationPolls > 1 ? 'located_mmdb' : 'queued',
          amount_fen: 888,
        })),
      };
    }
    await route.fulfill({ json: { code: 'OK', data } });
  });
  await page.goto('/admin');
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  return { previews };
}

async function openOrder(page: Page) {
  await page
    .getByRole('button', { name: /^订单详情 / })
    .first()
    .click();
  const dialog = page
    .getByRole('dialog')
    .filter({ has: page.getByRole('heading', { name: '订单详情', exact: true }) });
  await expect(dialog).toBeVisible();
  return dialog;
}

test('dialog action rows fill the available width at desktop and narrow viewports', async ({ page }) => {
  await setup(page);
  const dialog = await openOrder(page);
  for (const width of [1280, 520, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await expect(dialog).toHaveCSS('opacity', '1');
    const layout = await dialog.locator('.detail-actions').evaluate((el) => {
      const bounds = el.getBoundingClientRect();
      const rows = new Map<number, { left: number; right: number }>();
      for (const button of el.querySelectorAll(':scope > button')) {
        const box = button.getBoundingClientRect();
        const top = Math.round(box.top);
        const row = rows.get(top);
        rows.set(top, {
          left: Math.min(row?.left ?? box.left, box.left),
          right: Math.max(row?.right ?? box.right, box.right),
        });
      }
      return {
        left: bounds.left,
        right: bounds.right,
        rows: [...rows.values()],
        overflow: el.scrollWidth > el.clientWidth + 1,
      };
    });
    expect(layout.overflow, `action overflow at ${width}px`).toBe(false);
    expect(layout.rows.length).toBeGreaterThan(0);
    for (const row of layout.rows) {
      expect(Math.abs(row.left - layout.left), `row start at ${width}px`).toBeLessThanOrEqual(2);
      expect(Math.abs(row.right - layout.right), `row end at ${width}px`).toBeLessThanOrEqual(2);
    }
    if (width === 320) expect(layout.rows.length).toBeGreaterThan(1);
  }
  await dialog.getByRole('button', { name: '关闭', exact: true }).first().click();
  await page.locator('.a-side').getByRole('button', { name: '用户', exact: true }).click();
  await page
    .getByRole('button', { name: /^用户详情 / })
    .first()
    .click();
  const footer = page.getByRole('dialog').locator('.user-actions');
  const last = footer.getByRole('button').last();
  const row = await footer.boundingBox();
  const button = await last.boundingBox();
  if (!row || !button) throw new Error('User actions must remain visible on a narrow viewport');
  expect(Math.abs(button.x + button.width - (row.x + row.width))).toBeLessThanOrEqual(2);
});
