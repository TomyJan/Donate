import type { Page as BrowserPage } from '@playwright/test';
import { expect, test } from '@playwright/test';
import type { Audit, Order, Overview, User } from '../src/lib/types';

const users: User[] = Array.from({ length: 3 }, (_, index) => ({
  id: `admin-user-${index}`,
  nickname: `捐赠者 ${index}`,
  avatar_url: '',
  admin: false,
  status: 'active',
  bindings: [],
  created_at: '2026-10-29T08:00:00Z',
  last_login_at: '2026-10-30T08:00:00Z',
  order_count: 4,
  paid_fen: 400,
}));
const orders: Order[] = Array.from({ length: 12 }, (_, index) => ({
  id: `admin-order-${index}`,
  user_id: users[index % 3].id,
  nickname: users[index % 3].nickname,
  avatar_url: '',
  amount_fen: 100 + index,
  api_trade_no: '',
  channel: index % 2 ? 'wxpay' : 'alipay',
  close_reason: '',
  created_at: '2026-10-29T08:00:00Z',
  expires_at: '2026-11-01T08:00:00Z',
  gateway_status: '',
  hidden_message: false,
  hidden_order: false,
  merchant_order_no: `merchant-${index}`,
  message: '后台订单留言',
  protocol: 'epay',
  remote_close_status: '',
  status: 'paid',
  trade_no: '',
}));
const audit: Audit = {
  id: 'audit-1',
  actor: '管理员',
  actor_id: 'administrator',
  created_at: '2026-10-30T08:00:00Z',
  action: 'order.create',
  result: 'created',
  target: '捐赠者 0',
  target_exists: true,
  target_id: orders[0].id,
  target_type: 'order',
  target_amount_fen: 100,
  target_nickname: '捐赠者 0',
};

async function admin(page: BrowserPage) {
  let pause: Promise<void> | undefined;
  let release = () => {};
  let overviewCalls = 0;
  const orderQueries: URL[] = [];
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    let data: unknown = {};
    if (url.pathname.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'csrf',
        providers: ['oidc'],
        version: { built_at: '', commit: '', tag: '', version: 'test' },
      };
    if (url.pathname.endsWith('/user/profile'))
      data = {
        id: 'administrator',
        admin: true,
        avatar_url: '',
        nickname: '管理员',
        provider: 'oidc',
        bindings: [],
        csrf_token: 'csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (url.pathname.endsWith('/admin/overview')) {
      overviewCalls++;
      if (pause) await pause;
      data = {
        paid_fen: overviewCalls * 100,
        paid: 12,
        pending: 0,
        closed: 0,
        daily: Array.from({ length: 13 }, (_, i) => ({
          label: `2026-10-${String(i + 1).padStart(2, '0')}`,
          paid_fen: 100,
          count: 1,
        })),
        channels: [{ label: 'alipay', paid_fen: 100, count: 1 }],
      } satisfies Overview;
    }
    if (url.pathname.endsWith('/admin/user/detail'))
      data = users.find((user) => user.id === url.searchParams.get('id')) ?? users[0];
    if (url.pathname.endsWith('/admin/user/list')) {
      const search = url.searchParams.get('search') ?? '';
      const items = users.filter((user) => user.nickname.includes(search) || user.id.includes(search));
      data = { items, total: items.length, page: 1, page_size: 20 };
    }
    if (url.pathname.endsWith('/admin/order/list')) {
      orderQueries.push(url);
      const items = orders.filter(
        (order) => !url.searchParams.get('user_id') || order.user_id === url.searchParams.get('user_id'),
      );
      data = { items, total: items.length, page: 1, page_size: 20 };
    }
    if (url.pathname.endsWith('/admin/audit/list'))
      data = { items: [audit], total: 1, page: 1, page_size: 20 };
    await route.fulfill({ json: { code: 'OK', message: '', request_id: 'admin-followup', data } });
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/admin');
  await expect(page.locator('.overview-stats')).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  return {
    orderQueries,
    holdOverview: () => {
      pause = new Promise<void>((resolve) => {
        release = resolve;
      });
      return () => {
        const current = release;
        pause = undefined;
        current();
      };
    },
  };
}

test('cached admin rows show a refetch failure and recover with a local retry', async ({ page }) => {
  await page.clock.install();
  await admin(page);
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(12);
  let recovered = false;
  let requests = 0;
  await page.route('**/api/v1/admin/order/list?**', (route) => {
    requests++;
    return recovered
      ? route.fulfill({
          json: { code: 'OK', data: { items: orders, total: orders.length, page: 1, page_size: 20 } },
        })
      : route.fulfill({ status: 503, contentType: 'text/plain', body: 'proxy unavailable' });
  });
  await page.locator('.a-side').getByRole('button', { name: '概览', exact: true }).click();
  await expect(page.locator('.overview-stats')).toBeVisible();
  await page.clock.fastForward(16000);
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  await expect.poll(() => requests).toBeGreaterThan(0);
  const error = page.locator('.a-main .request-error');
  await expect(error).toContainText('服务器正在忙');
  await expect(page.locator('tbody tr')).toHaveCount(12);
  recovered = true;
  await error.getByRole('button', { name: '重试', exact: true }).click();
  await expect(error).toHaveCount(0);
  await expect(page.locator('tbody tr')).toHaveCount(12);
});

test('admin tab navigation keeps its intrinsic height across short and long pages', async ({ page }) => {
  await admin(page);
  for (const height of [600, 1000])
    for (const width of [320, 390, 768, 1100]) {
      await page.setViewportSize({ width, height });
      const sizes = [];
      for (const tab of ['概览', '订单', '用户', '审计']) {
        await page.locator('.a-side').getByRole('button', { name: tab, exact: true }).click();
        await expect(page.locator('.a-main h1')).toHaveText(tab);
        if (tab === '概览') await expect(page.locator('.overview-stats')).toBeVisible();
        else await expect(page.locator('tbody tr').first()).toBeVisible();
        sizes.push(
          await page.locator('.a-side').evaluate((el) => {
            const layout = el.parentElement;
            if (!layout) throw new Error('Admin navigation lost its shared layout');
            return {
              height: el.getBoundingClientRect().height,
              layout: getComputedStyle(layout).display,
              rows: getComputedStyle(layout).gridTemplateRows,
              shrink: getComputedStyle(el).flexShrink,
            };
          }),
        );
      }
      expect(
        Math.max(...sizes.map((size) => size.height)) - Math.min(...sizes.map((size) => size.height)),
        `${width}x${height}: ${JSON.stringify(sizes)}`,
      ).toBeLessThan(0.5);
    }
});

test('changing overview range and refreshing retain cards and metric pagination during the request', async ({
  page,
}) => {
  const control = await admin(page);
  const card = await page.locator('.overview-stats').elementHandle();
  if (!card) throw new Error('Overview cards missing');
  await page
    .locator('.overview-grid > section')
    .first()
    .getByRole('button', { name: '下一页', exact: true })
    .click();
  const release = control.holdOverview();
  try {
    await page.getByRole('combobox', { name: '时间范围', exact: true }).click();
    await page.getByRole('option', { name: '7d', exact: true }).click();
    await expect(page.locator('.overview-stats')).toBeVisible();
    expect(await card.evaluate((el) => el.isConnected)).toBe(true);
    await expect(page.locator('.overview-grid > section').first()).toContainText('2 / 2');
    await expect(page.locator('.overview-progress')).toBeVisible();
  } finally {
    release();
  }
  await expect(page.locator('.overview-progress')).toHaveCount(0);
  expect(await card.evaluate((el) => el.isConnected)).toBe(true);
  const finish = control.holdOverview();
  try {
    await page.locator('.overview-refresh').click();
    await expect(page.locator('.overview-refresh')).toBeDisabled();
    expect(await card.evaluate((el) => el.isConnected)).toBe(true);
  } finally {
    finish();
  }
});

test('overview range and refresh share one heading row at every viewport width', async ({ page }) => {
  await admin(page);
  for (const width of [320, 390, 768, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    const actions = page.locator('.a-heading-actions');
    await expect(actions.locator('.overview-refresh')).toBeVisible();
    const range = await actions.getByRole('combobox', { name: '时间范围', exact: true }).boundingBox();
    const refresh = await actions.locator('.overview-refresh').boundingBox();
    if (!range || !refresh) throw new Error('Missing overview controls');
    expect(Math.abs(range.y + range.height / 2 - refresh.y - refresh.height / 2)).toBeLessThan(1);
    expect(refresh.x + refresh.width).toBeLessThanOrEqual(width);
    await expect(page.locator('.overview-controls')).toHaveCount(0);
  }
});

test('view donations uses the regular user filter without an extra toolbar row', async ({ page }) => {
  const control = await admin(page);
  await page.locator('.a-side').getByRole('button', { name: '用户', exact: true }).click();
  await page
    .getByRole('button', { name: /^用户详情 / })
    .first()
    .click();
  await expect(page.locator('.user-history')).toHaveCount(0);
  await page.getByRole('button', { name: '查看捐赠', exact: true }).click();
  await expect(page.locator('.user-filter')).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: '用户筛选', exact: true })).toContainText('捐赠者 0');
  await expect.poll(() => control.orderQueries.at(-1)?.searchParams.get('user_id')).toBe(users[0].id);
});

test('failed overview range requests retain previous metrics and pagination', async ({ page }) => {
  await admin(page);
  const card = await page.locator('.overview-stats').elementHandle();
  if (!card) throw new Error('Overview cards missing');
  const daily = page.locator('.overview-grid > section').first();
  await daily.getByRole('button', { name: '下一页', exact: true }).click();
  const amount = await page.locator('.overview-stats strong').first().textContent();
  await page.route('**/api/v1/admin/overview?**', (route) =>
    route.fulfill({
      json: { code: 'UPSTREAM_UNAVAILABLE', message: '', request_id: 'failed-overview', data: null },
    }),
  );
  await page.getByRole('combobox', { name: '时间范围', exact: true }).click();
  await page.getByRole('option', { name: '7d', exact: true }).click();
  await expect(page.locator('.toast').filter({ hasText: '第三方暂未响应' })).toBeVisible();
  expect(await card.evaluate((el) => el.isConnected)).toBe(true);
  await expect(page.locator('.overview-stats strong').first()).toHaveText(amount ?? '');
  await expect(daily).toContainText('2 / 2');
  await expect(page.locator('.overview-refresh')).toBeEnabled();
});

test('advanced date and amount validation runs on apply and reports through toasts', async ({ page }) => {
  const control = await admin(page);
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  await page.getByRole('button', { name: /^筛选/ }).click();
  for (const name of ['开始日期', '结束日期']) {
    await page.getByRole('button', { name, exact: true }).click();
    await page.locator('.date-pop').getByRole('combobox', { name: 'Hour' }).click();
    await page.getByRole('option', { name: '08', exact: true }).click();
    await page.locator('.date-pop').getByRole('button', { name: '确认', exact: true }).click();
    await expect(page.locator('.date-pop')).toHaveCount(0);
  }
  await expect(page.locator('.a-filters [role=alert]')).toHaveCount(0);
  await page.locator('.a-filters').getByRole('button', { name: '应用', exact: true }).click();
  await expect(page.locator('.toast').filter({ hasText: '开始时间必须早于结束时间' })).toBeVisible();
  expect(
    control.orderQueries.every((url) => !url.searchParams.has('from') && !url.searchParams.has('to')),
  ).toBe(true);
  await page.locator('.a-filters').getByRole('button', { name: '重置', exact: true }).click();
  await page.getByRole('textbox', { name: '最低金额', exact: true }).fill('20');
  await page.getByRole('textbox', { name: '最高金额', exact: true }).fill('10');
  await page.locator('.a-filters').getByRole('button', { name: '应用', exact: true }).click();
  await expect(page.locator('.toast').filter({ hasText: '最低金额不能高于最高金额' })).toBeVisible();
});

test('user search and responsive filter movement retain one controlled filter with stable clearing height', async ({
  page,
}) => {
  const control = await admin(page);
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  const user = page.getByRole('combobox', { name: '用户筛选', exact: true });
  await user.click();
  await page.getByRole('textbox', { name: '搜索用户', exact: true }).fill('捐赠者 1');
  await page.getByRole('option', { name: /捐赠者 1/ }).click();
  await expect.poll(() => control.orderQueries.at(-1)?.searchParams.get('user_id')).toBe(users[1].id);
  await page.getByRole('combobox', { name: '状态', exact: true }).click();
  await page.getByRole('option', { name: '已支付', exact: true }).click();
  await page.setViewportSize({ width: 320, height: 900 });
  await expect(page.locator('.a-order-tools')).toBeVisible();
  const toolbarHeight = await page
    .locator('.a-order-tools')
    .evaluate((el) => el.getBoundingClientRect().height);
  await page.getByRole('button', { name: /^筛选/ }).click();
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toHaveCount(1);
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toContainText('已支付');
  await expect(user).toHaveCount(1);
  await expect(user).toContainText('捐赠者 1');
  await user.click();
  await page.getByRole('option', { name: '全部用户', exact: true }).click();
  await expect.poll(() => control.orderQueries.at(-1)?.searchParams.get('user_id')).toBeNull();
  expect(await page.locator('.a-order-tools').evaluate((el) => el.getBoundingClientRect().height)).toBe(
    toolbarHeight,
  );
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(user).toHaveCount(1);
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toHaveCount(1);
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toContainText('已支付');
});

test('order visibility badges stay beside the amount without overflowing narrow dialogs', async ({
  page,
}) => {
  await admin(page);
  await page.route('**/api/v1/admin/order/detail?**', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: { ...orders[0], amount_fen: 10000000, hidden_order: true, hidden_message: true },
      },
    }),
  );
  await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
  await page
    .getByRole('button', { name: /^订单详情 / })
    .first()
    .click();
  const dialog = page.getByRole('dialog', { name: '订单详情', exact: true });
  for (const width of [320, 390, 768, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(dialog.locator('.order-visibility')).toHaveText('订单隐藏留言隐藏');
    const positions = await dialog.locator('.order-amount-row').evaluate((row) => {
      const amount = row.querySelector('.order-amount')?.getBoundingClientRect();
      const flags = row.querySelector('.order-visibility')?.getBoundingClientRect();
      return { amount, flags, overflow: row.scrollWidth > row.clientWidth + 1 };
    });
    expect(positions.overflow).toBe(false);
    expect(positions.flags?.left).toBeGreaterThanOrEqual(positions.amount?.right ?? 0);
    expect(await dialog.evaluate((el) => el.scrollWidth > el.clientWidth + 1)).toBe(false);
  }
  await page.screenshot({ path: 'test-results/admin-order-visibility.png' });
});

test('user details show registration, donation amount and binding times with managed unbinding', async ({
  page,
}) => {
  await admin(page);
  let bindings = ['qq', 'oidc'].map((provider, index) => ({
    id: `identity-${index}`,
    provider,
    nickname: `${provider} account`,
    avatar_url: '',
    bound_at: `2026-10-${index ? '29' : '28'}T08:00:00Z`,
  }));
  const requests: unknown[] = [];
  await page.route('**/api/v1/admin/user/detail?**', (route) =>
    route.fulfill({ json: { code: 'OK', data: { ...users[0], bindings } } }),
  );
  await page.route('**/api/v1/admin/user/identity/unbind', (route) => {
    const body = route.request().postDataJSON();
    requests.push(body);
    bindings = bindings.filter((binding) => binding.id !== body.identity_id);
    return route.fulfill({ json: { code: 'OK', data: null } });
  });
  await page.locator('.a-side').getByRole('button', { name: '用户', exact: true }).click();
  await expect(page.getByRole('button', { name: '注册时间', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '捐赠金额', exact: true })).toBeVisible();
  await page.setViewportSize({ width: 320, height: 900 });
  await expect(page.locator('tbody tr').first().locator('[data-field=created_at]')).toHaveAttribute(
    'data-label',
    '注册时间',
  );
  await page
    .getByRole('button', { name: /^用户详情 / })
    .first()
    .click();
  const dialog = page.getByRole('dialog', { name: '用户详情', exact: true });
  await expect(dialog.locator('.user-properties')).toContainText('捐赠金额¥4.00');
  await expect(dialog.locator('.d-bind-time time')).toHaveCount(2);
  await expect(dialog.locator('.d-bind-time time').first()).toHaveAttribute(
    'datetime',
    '2026-10-28T08:00:00Z',
  );
  for (const width of [320, 390, 768, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await dialog.evaluate((el) => el.scrollWidth > el.clientWidth + 1)).toBe(false);
    await expect(dialog.getByRole('button', { name: '解绑 QQ', exact: true })).toBeVisible();
  }
  await page.screenshot({ path: 'test-results/admin-user-bindings.png' });
  await dialog.getByRole('button', { name: '解绑 QQ', exact: true }).click();
  await page.locator('.action-confirm').getByRole('button', { name: '确认', exact: true }).click();
  await expect.poll(() => requests).toEqual([{ id: users[0].id, identity_id: 'identity-0' }]);
  await expect(dialog.locator('.d-bind-time time')).toHaveCount(1);
  await expect(dialog.getByRole('button', { name: '解绑 OIDC', exact: true })).toBeDisabled();
});

for (const lang of ['zh-CN', 'en'] as const) {
  test(`${lang}: long provider names keep identity information readable beside compact unlink actions`, async ({
    page,
  }) => {
    await page.addInitScript((value) => localStorage.setItem('donate-language', value), lang);
    await admin(page);
    let providerName = 'TomyJan 统一身份认证中心';
    await page.route('**/api/v1/site/info', async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      body.data.providers = ['campus', 'qq'];
      body.data.provider_details = [
        { id: 'campus', kind: 'oidc', name: providerName, icon_url: '' },
        { id: 'qq', kind: 'qq', name: 'QQ', icon_url: '' },
      ];
      await route.fulfill({ json: body });
    });
    await page.route('**/api/v1/admin/user/detail?**', (route) =>
      route.fulfill({
        json: {
          code: 'OK',
          data: {
            ...users[0],
            bindings: ['qq', 'oidc'].map((provider, index) => ({
              id: `identity-${index}`,
              provider,
              nickname: 'Donor',
              avatar_url: '',
              bound_at: '2026-10-30T00:00:00Z',
            })),
          },
        },
      }),
    );
    for (const name of ['TomyJan 统一身份认证中心', 'a'.repeat(64)]) {
      providerName = name;
      await page.reload();
      await page
        .locator('.a-side')
        .getByRole('button', { name: lang === 'en' ? 'Users' : '用户', exact: true })
        .click();
      await page
        .getByRole('button', { name: lang === 'en' ? /^User details / : /^用户详情 / })
        .first()
        .click();
      const dialog = page.getByRole('dialog', {
        name: lang === 'en' ? 'User details' : '用户详情',
        exact: true,
      });
      const binding = dialog.locator('.d-bind').last();
      const unlink = binding.getByRole('button', {
        name: `${lang === 'en' ? 'Unlink' : '解绑'} ${name}`,
        exact: true,
      });
      for (const width of [320, 390, 1280]) {
        await page.setViewportSize({ width, height: 900 });
        await expect(unlink).toBeVisible();
        await expect
          .poll(() => binding.locator('.d-bind-info').evaluate((el) => el.getBoundingClientRect().width))
          .toBeGreaterThanOrEqual(100);
        expect(await unlink.evaluate((el) => el.getBoundingClientRect().width)).toBeLessThanOrEqual(100);
        expect(await binding.evaluate((el) => el.getBoundingClientRect().height)).toBeLessThan(360);
        expect(await dialog.evaluate((el) => el.scrollWidth > el.clientWidth + 1)).toBe(false);
      }
      await expect(unlink).toHaveText(lang === 'en' ? 'Unlink' : '解绑');
      await unlink.click();
      await expect(
        page.getByRole('dialog', { name: `${lang === 'en' ? 'Unlink' : '解绑'} ${name}`, exact: true }),
      ).toContainText(name);
    }
  });
}

test('admin navigation animates content and selection without resetting the active page', async ({
  page,
}) => {
  await admin(page);
  await page.evaluate(() => {
    document.documentElement.dataset.adminEntrance = 'pending';
    document.addEventListener('animationstart', (event) => {
      if (!(event.target instanceof HTMLElement) || !event.target.matches('.a-main')) return;
      const moving = event.target
        .getAnimations()
        .some((animation) =>
          (animation.effect as KeyframeEffect)
            .getKeyframes()
            .some((frame) => frame.transform && frame.transform !== 'none'),
        );
      document.documentElement.dataset.adminEntrance = moving ? 'moving' : 'static';
    });
  });
  const previous = await page.locator('.a-main').elementHandle();
  const ordersTab = page.locator('.a-side').getByRole('button', { name: '订单', exact: true });
  await ordersTab.click();
  expect(await previous?.evaluate((element) => element.isConnected)).toBe(false);
  const main = await page.locator('.a-main').elementHandle();
  await expect(page.locator('html')).toHaveAttribute('data-admin-entrance', 'moving');
  await expect(ordersTab.locator('.d-selection-highlight')).toBeVisible();
  const search = page.getByRole('textbox', { name: '昵称 / 订单 ID / 用户 ID', exact: true });
  await search.fill('保留筛选');
  await ordersTab.click();
  await expect(search).toHaveValue('保留筛选');
  expect(await main?.evaluate((element) => element.isConnected)).toBe(true);
  await ordersTab.focus();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await expect(ordersTab).toBeFocused();
  await expect(ordersTab).toHaveCSS('outline-style', 'solid');
  const box = await ordersTab.boundingBox();
  if (!box) throw new Error('Missing navigation button');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  try {
    await expect(ordersTab).toHaveCSS('transform', 'none');
    await expect
      .poll(() =>
        ordersTab
          .locator('.d-selection-content')
          .evaluate((content) => Number.parseFloat(getComputedStyle(content).scale)),
      )
      .toBeLessThan(0.99);
  } finally {
    await page.mouse.up();
  }
});

for (const width of [1280, 768, 320])
  test(`navigation highlight stays above every button surface in both directions at ${width}px`, async ({
    page,
  }) => {
    await admin(page);
    await page.setViewportSize({ width, height: 900 });
    for (const label of ['审计', '概览']) {
      await page.locator('.a-side').evaluate((nav) => {
        nav.removeAttribute('data-highlight-samples');
        nav.addEventListener(
          'click',
          () => {
            const samples: { covered: boolean; labelCovered: boolean; top: string }[] = [];
            const sample = () => {
              const indicator = nav.querySelector<HTMLElement>('.d-selection-highlight');
              if (indicator) {
                const rect = indicator.getBoundingClientRect();
                const previous = indicator.style.pointerEvents;
                // Hit testing changes no paint or stacking, and is restored within this frame.
                indicator.style.pointerEvents = 'auto';
                const top = document.elementFromPoint(rect.x + rect.width / 2, rect.y + 4);
                let labelCovered = false;
                for (const content of nav.querySelectorAll<HTMLElement>('.d-selection-content')) {
                  const box = content.getBoundingClientRect();
                  const x = box.x + box.width / 2,
                    y = box.y + box.height / 2;
                  if (x < rect.left || x > rect.right || y < rect.top || y > rect.bottom) continue;
                  const previousContent = content.style.pointerEvents;
                  content.style.pointerEvents = 'auto';
                  const text = document.elementFromPoint(x, y);
                  labelCovered ||= !text || !content.contains(text);
                  content.style.pointerEvents = previousContent;
                }
                indicator.style.pointerEvents = previous;
                samples.push({
                  covered: top !== indicator,
                  labelCovered,
                  top: top?.className?.toString() ?? '',
                });
              }
              if (samples.length < 24) requestAnimationFrame(sample);
              else nav.setAttribute('data-highlight-samples', JSON.stringify(samples));
            };
            requestAnimationFrame(sample);
          },
          { once: true },
        );
      });
      await page.locator('.a-side').getByRole('button', { name: label, exact: true }).click();
      const nav = page.locator('.a-side');
      await expect(nav).toHaveAttribute('data-highlight-samples', /.+/);
      const samples = JSON.parse((await nav.getAttribute('data-highlight-samples')) ?? '[]') as {
        covered: boolean;
        labelCovered: boolean;
        top: string;
      }[];
      expect(
        samples.filter((sample) => sample.covered),
        `${label}: moving highlight must remain above all button surfaces`,
      ).toEqual([]);
      expect(
        samples.some((sample) => sample.labelCovered),
        'labels must stay above the moving highlight',
      ).toBe(false);
    }
  });

test('reduced motion suppresses admin entrance and navigation movement', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await admin(page);
  for (const label of ['订单', '用户', '审计', '概览']) {
    await page.locator('.a-side').getByRole('button', { name: label, exact: true }).click();
    await expect(page.locator('.a-main h1')).toHaveText(label);
    expect(await page.locator('.a-main').evaluate((element) => element.getAnimations().length)).toBe(0);
  }
});

test('order and user dialogs keep a responsive skeleton while details are loading', async ({ page }) => {
  await admin(page);
  for (const kind of ['order', 'user'] as const) {
    let release = () => {};
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(`**/api/v1/admin/${kind}/detail?**`, async (route) => {
      await pending;
      await route.fulfill({ json: { code: 'OK', data: kind === 'order' ? orders[0] : users[0] } });
    });
    await page
      .locator('.a-side')
      .getByRole('button', { name: kind === 'order' ? '订单' : '用户', exact: true })
      .click();
    await page
      .getByRole('button', { name: kind === 'order' ? /^订单详情 / : /^用户详情 / })
      .first()
      .click();
    const dialog = page.getByRole('dialog', {
      name: kind === 'order' ? '订单详情' : '用户详情',
      exact: true,
    });
    try {
      const loading = dialog.getByRole('status', { name: '加载中', exact: true });
      await expect(loading.locator('.admin-loading-detail')).toBeVisible();
      for (const width of [320, 1280]) {
        await page.setViewportSize({ width, height: 900 });
        expect(await dialog.evaluate((element) => element.scrollWidth > element.clientWidth + 1)).toBe(false);
      }
      release();
      await expect(loading).toHaveCount(0);
      await expect(
        dialog.getByText(kind === 'order' ? orders[0].nickname : users[0].nickname, { exact: true }),
      ).toBeVisible();
    } finally {
      release();
    }
    await dialog.getByRole('button', { name: '关闭', exact: true }).click();
    await expect(dialog).toHaveCount(0);
  }
});
