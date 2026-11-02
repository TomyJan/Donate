import { expect, type Page, test } from '@playwright/test';
import type { Order, User } from '../src/lib/types';

const targetID = '018f0101-0000-7000-8000-000000000001';
const original: Order = {
  id: '018f0101-0000-7000-8000-000000000002',
  user_id: '',
  nickname: '历史捐赠者',
  avatar_url: '',
  amount_fen: 888,
  channel: 'qqpay',
  status: 'paid',
  protocol: 'legacy',
  merchant_order_no: 'legacy-migration-browser',
  api_trade_no: '',
  trade_no: '',
  message: '保留原有留言',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
  remote_close_status: '',
  created_at: '2022-01-01T08:00:01Z',
  expires_at: '2022-01-01T08:30:01Z',
  paid_at: '2022-01-01T08:01:02Z',
  ip: '192.0.2.1',
  province: '广东',
};
const target: User = {
  id: targetID,
  nickname: '现有用户',
  avatar_url: '',
  status: 'active',
  admin: false,
  bindings: [],
  created_at: '2026-10-29T08:00:00Z',
  last_login_at: '2026-10-30T08:00:00Z',
  order_count: 0,
  paid_fen: 0,
};

async function setup(page: Page, failFirst = false) {
  let order = { ...original };
  const requests: { id: string; user_id: string }[] = [];
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
        id: 'admin',
        nickname: '管理员',
        avatar_url: '',
        admin: true,
        provider: 'oidc',
        bindings: [],
        csrf_token: 'csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/admin/overview'))
      data = { paid_fen: 888, paid: 1, pending: 0, closed: 0, daily: [], channels: [] };
    if (path.endsWith('/admin/order/list')) data = { items: [order], total: 1, page: 1, page_size: 20 };
    if (path.endsWith('/admin/order/detail')) data = order;
    if (path.endsWith('/admin/user/list')) data = { items: [target], total: 1, page: 1, page_size: 20 };
    if (path.endsWith('/admin/user/detail')) data = target;
    if (path.endsWith('/admin/order/migrate')) {
      requests.push(route.request().postDataJSON());
      expect(route.request().method()).toBe('POST');
      expect(route.request().headers()['x-csrf-token']).toBe('csrf');
      if (failFirst && requests.length === 1) {
        await route.fulfill({ status: 503, json: { code: 'INTERNAL_ERROR', message: 'private upstream' } });
        return;
      }
      order = { ...order, user_id: targetID };
      data = { id: order.id, user_id: targetID, result: 'migrated' };
    }
    await route.fulfill({ json: { code: 'OK', message: '', request_id: 'migration-test', data } });
  });
  await page.goto('/admin');
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  await page
    .getByRole('button', { name: /订单详情/ })
    .first()
    .click();
  const detail = page.getByRole('dialog', { name: '订单详情', exact: true });
  await expect(detail).toBeVisible();
  return { detail, requests };
}

for (const width of [1280, 320]) {
  test(`legacy migration selects an existing user and confirms before changing ownership at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const { detail, requests } = await setup(page);
    await detail.getByRole('button', { name: '迁移订单', exact: true }).click();
    const migration = page.locator('.order-migration');
    await migration.getByRole('combobox', { name: '目标用户', exact: true }).click();
    const picker = page.locator('.admin-user-pop');
    const search = picker.getByRole('textbox', { name: '搜索用户', exact: true });
    await expect(search).toBeVisible();
    const searchSize = await picker.locator('.admin-user-search').boundingBox();
    expect(searchSize?.height).toBeGreaterThanOrEqual(40);
    expect(searchSize?.height).toBeLessThanOrEqual(48);
    expect(await picker.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
    const option = picker.getByRole('option', { name: new RegExp(target.nickname) });
    const inset = await option.evaluate((el) => {
      const rect = el.getBoundingClientRect();
      const list = el.parentElement?.getBoundingClientRect();
      if (!list) throw new Error('Missing user list');
      return { left: rect.left - list.left, right: list.right - rect.right };
    });
    expect(inset.left).toBeGreaterThanOrEqual(4);
    expect(inset.right).toBeGreaterThanOrEqual(4);
    await page.screenshot({ path: info.outputPath(`migration-user-picker-${width}.png`) });
    await page.getByRole('option', { name: new RegExp(target.nickname) }).click();
    await expect(migration.locator('.order-migration-target')).toContainText(targetID);
    expect(requests).toEqual([]);
    await migration.getByRole('button', { name: '检查迁移', exact: true }).click();
    expect(requests).toEqual([]);
    await expect(migration).toBeVisible();
    expect(
      await migration.evaluate(
        (el) => el.getBoundingClientRect().left >= 0 && el.getBoundingClientRect().right <= innerWidth,
      ),
    ).toBe(true);
    expect(await migration.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath(`migration-${width}.png`), fullPage: true });
    await migration.getByRole('button', { name: '确认', exact: true }).click();
    await expect(detail.getByRole('button', { name: '迁移订单', exact: true })).toHaveCount(0);
    await expect(detail).toContainText(targetID);
    await expect(detail).toContainText(original.nickname);
    await expect(detail).toContainText(original.message);
    expect(requests).toEqual([{ id: original.id, user_id: targetID }]);
  });
}

test('failed migration keeps the selected target available for an explicit retry', async ({ page }) => {
  const { detail, requests } = await setup(page, true);
  await detail.getByRole('button', { name: '迁移订单', exact: true }).click();
  const migration = page.locator('.order-migration');
  await migration.getByRole('combobox', { name: '目标用户', exact: true }).click();
  await page.getByRole('option', { name: new RegExp(target.nickname) }).click();
  await migration.getByRole('button', { name: '检查迁移', exact: true }).click();
  await migration.getByRole('button', { name: '确认', exact: true }).click();
  await expect(migration.getByRole('button', { name: '检查迁移', exact: true })).toBeEnabled();
  await expect(migration.locator('.order-migration-target')).toContainText(targetID);
  await expect(migration).not.toContainText('private upstream');
  expect(requests).toHaveLength(1);
  await migration.getByRole('button', { name: '检查迁移', exact: true }).click();
  await migration.getByRole('button', { name: '确认', exact: true }).click();
  await expect(detail.getByRole('button', { name: '迁移订单', exact: true })).toHaveCount(0);
  expect(requests).toHaveLength(2);
});

for (const side of ['top', 'bottom'] as const) {
  test(`confirmation motion follows actual ${side} placement after collision detection`, async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const { detail } = await setup(page);
    // Put the real triggers near each viewport edge so Radix must choose opposite sides.
    await page.addStyleTag({
      content: `
        .d-app .d-dialog { top: 12px; transform: translateX(-50%); height: calc(100vh - 24px); max-height: none; }
        .d-app .detail-actions { position: absolute; left: 26px; right: 26px; top: ${side === 'bottom' ? '12px' : 'auto'}; bottom: ${side === 'top' ? '12px' : 'auto'}; }
        .d-app .d-pop[data-state] { animation-duration: 10s; }
      `,
    });
    for (const label of ['迁移订单', '删除订单']) {
      await detail.getByRole('button', { name: label, exact: true }).click();
      const popover = page.locator('.action-confirm');
      await expect(popover).toHaveAttribute('data-side', side);
      const motion = await popover.evaluate((el) => {
        const animation = el
          .getAnimations()
          .find((item) => item instanceof CSSAnimation && item.animationName === 'pop-in');
        if (!animation || !(animation.effect instanceof KeyframeEffect))
          throw new Error('Missing enter animation');
        animation.pause();
        return animation.effect.getKeyframes()[0].translate;
      });
      expect(motion).toBe(side === 'top' ? '0px 5px' : '0px -5px');
      await popover.getByRole('button', { name: '取消', exact: true }).click();
      // Finish the deliberately slowed exit so the next trigger can receive pointer events.
      await popover.evaluate((el) => {
        for (const animation of el.getAnimations()) animation.finish();
      });
      await expect(popover).toHaveCount(0);
    }
  });
}

for (const width of [1280, 320]) {
  test(`the shared order filter user picker stays compact at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const { detail } = await setup(page);
    await detail.getByRole('button', { name: '关闭', exact: true }).click();
    const trigger = page.getByRole('combobox', { name: '用户筛选', exact: true });
    if (!(await trigger.isVisible())) await page.locator('.a-filter-toggle').click();
    await trigger.click();
    const picker = page.locator('.admin-user-pop');
    await expect(picker.getByRole('textbox', { name: '搜索用户', exact: true })).toBeVisible();
    const size = await picker.locator('.admin-user-search').boundingBox();
    expect(size?.height).toBeGreaterThanOrEqual(40);
    expect(size?.height).toBeLessThanOrEqual(48);
    expect(await picker.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
    await picker.getByRole('option', { name: new RegExp(target.nickname) }).click();
    await expect(trigger).toContainText(target.nickname);
  });
}

test.describe('touch user picker', () => {
  test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 900 } });

  test('search keeps a compact touch target and readable text', async ({ page }) => {
    const { detail } = await setup(page);
    await detail.getByRole('button', { name: '迁移订单', exact: true }).click();
    await page.locator('.order-migration').getByRole('combobox').click();
    const search = page.locator('.admin-user-search input');
    await expect(search).toHaveCSS('font-size', '16px');
    await expect(search).toHaveCSS('min-height', '44px');
    const size = await page.locator('.admin-user-search').boundingBox();
    expect(size?.height).toBeLessThanOrEqual(48);
  });
});
