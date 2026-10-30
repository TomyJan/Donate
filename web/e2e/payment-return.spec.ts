import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';

const orderID = '68f15e68-143e-4b30-a2a9-87c0df4c5479';
const userID = '11644e41-c098-4c32-b44e-96926c19df18';
const paidOrder = {
  id: orderID,
  user_id: userID,
  nickname: '付款测试',
  avatar_url: '',
  amount_fen: 888,
  channel: 'alipay',
  status: 'paid',
  message: '这张收据会归入记录。',
  province: '广东',
  created_at: '2026-10-30T08:00:00Z',
  paid_at: '2026-10-30T08:01:00Z',
  expires_at: '2026-10-30T08:30:00Z',
  merchant_order_no: 'return-test',
  trade_no: 'gateway-confirmed',
  api_trade_no: '',
  protocol: 'v2',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
};
async function prepare(page: Page, options: { status?: string; error?: string; signedIn?: boolean } = {}) {
  let paid = options.status !== 'pending';
  let failed = !!options.error;
  await page.addInitScript(() => {
    localStorage.setItem('donate:cookie-notice', '2026-10-30');
    sessionStorage.setItem(
      'donate-draft',
      JSON.stringify({ amount: '8.88', channel: 'alipay', message: '保留的草稿' }),
    );
  });
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/user/profile') && options.signedIn === false)
      return route.fulfill({ status: 401, json: { code: 'UNAUTHORIZED', data: null } });
    if (path.endsWith('/order/detail')) {
      if (failed) return route.fulfill({ json: { code: options.error, data: null } });
      return route.fulfill({
        json: { code: 'OK', data: { ...paidOrder, status: paid ? 'paid' : 'pending' } },
      });
    }
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        providers: ['oidc'],
        payment_enabled: true,
        version: { version: 'test', commit: '', tag: '' },
      };
    if (path.endsWith('/user/profile'))
      data = {
        id: userID,
        nickname: '付款测试',
        avatar_url: '',
        bindings: [],
        admin: false,
        csrf_token: 'test',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/order/list')) data = { items: [], next_cursor: '' };
    await route.fulfill({ json: { code: 'OK', data } });
  });
  return {
    paid: () => {
      paid = true;
    },
    recover: () => {
      failed = false;
    },
  };
}

test('a verified payment pauses for a second, slowly files the receipt and restores a fresh composer', async ({
  page,
}, info) => {
  const gateway = await prepare(page, { status: 'pending' });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  const pay = ticket.locator('.r-pay > .r-primary');
  await expect(pay).toBeDisabled();
  await expect(pay).toContainText('查询');
  gateway.paid();
  await expect(ticket).toHaveAttribute('data-payment-phase', 'confirmed', { timeout: 7000 });
  const confirmedAt = Date.now();
  await expect(pay).toContainText('查询成功');
  await page.screenshot({ path: info.outputPath('confirmed.png') });
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  expect(Date.now() - confirmedAt).toBeGreaterThanOrEqual(750);
  const travelAt = Date.now();
  await page.screenshot({ path: info.outputPath('folding.png') });
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete', { timeout: 8000 });
  expect(Date.now() - travelAt).toBeGreaterThan(2200);
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('这张收据会归入记录');
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('广东');
  await expect(pay).toBeEnabled();
  await expect(ticket.locator('textarea')).toHaveValue('');
  await expect(page).toHaveURL('/');
  await page.screenshot({ path: info.outputPath('filed.png') });
  await page.reload();
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'idle');
});

test('pending payment is not declared successful from return parameters and permits a later check', async ({
  page,
}) => {
  const gateway = await prepare(page, { status: 'pending' });
  await page.goto(`/?payment=returned&order=${orderID}&trade_status=TRADE_SUCCESS`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'checking');
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  await expect(page.locator('.r-featured')).toHaveCount(0);
  await page.getByRole('button', { name: '稍后查看', exact: true }).click();
  await expect(page.locator('.r-pay > .r-primary')).toBeEnabled();
  await expect(page.locator('.r-message')).toHaveValue('保留的草稿');
  gateway.paid();
});

test('an unavailable order offers recovery instead of success or duplicate payment', async ({ page }) => {
  const gateway = await prepare(page, { error: 'NOT_FOUND' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.payment-return-status')).toContainText('不存在');
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  gateway.recover();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.getByRole('button', { name: '重新查询', exact: true }).click();
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('8.88');
});

test('a signed-out return requests sign-in without querying private order data', async ({ page }) => {
  await prepare(page, { signedIn: false });
  const requests: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/order/detail')) requests.push(request.url());
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.payment-return-status')).toContainText('登录');
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  await page.locator('.payment-return-status').getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '登录', exact: true })).toBeVisible();
  expect(requests).toEqual([]);
});

test('a narrow-screen reduced-motion return completes without a flying paper or horizontal overflow', async ({
  page,
}) => {
  await prepare(page);
  await page.setViewportSize({ width: 320, height: 720 });
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('8.88');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.locator('.r-pay > .r-primary')).toBeEnabled();
});

test('automatic checking stops with an actionable pending state instead of disabling forever', async ({
  page,
}) => {
  await prepare(page, { status: 'pending' });
  await page.clock.install();
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'checking');
  await page.clock.fastForward(46000);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'pending');
  await expect(page.getByRole('button', { name: '重新查询', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '稍后查看', exact: true }).click();
  await expect(page.locator('.r-pay > .r-primary')).toBeEnabled();
});

test('a readable payment owned by somebody else cannot become a personal success receipt', async ({
  page,
}) => {
  await prepare(page);
  await page.route('**/api/v1/order/detail?**', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: { ...paidOrder, user_id: 'another-user' },
      },
    }),
  );
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'error');
  await expect(page.locator('.payment-return-status')).toContainText('无权访问');
  await expect(page.locator('.r-featured')).toHaveCount(0);
});

test('a phone receipt follows the document to the record without creating sideways scroll', async ({
  page,
}, info) => {
  await prepare(page);
  await page.setViewportSize({ width: 320, height: 720 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  const samples = await page.evaluate(async () => {
    const frames: { overflow: number; top: number; transform: string }[] = [];
    while (
      document.querySelector('.r-write .r-ticket')?.getAttribute('data-payment-phase') === 'filing' &&
      frames.length < 300
    ) {
      const el = document.querySelector('.r-write .r-ticket');
      if (!el) break;
      frames.push({
        overflow: document.documentElement.scrollWidth - innerWidth,
        top: el.getBoundingClientRect().top,
        transform: getComputedStyle(el).transform,
      });
      await new Promise(requestAnimationFrame);
    }
    return frames;
  });
  expect(samples.length).toBeGreaterThan(30);
  expect(Math.max(...samples.map((frame) => frame.overflow))).toBeLessThanOrEqual(0);
  expect(new Set(samples.map((frame) => frame.transform)).size).toBeGreaterThan(20);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('这张收据会归入记录');
  await page.screenshot({ path: info.outputPath('phone-filed-dark.png') });
});

test('fresh server records replace the temporary receipt snapshot after payment', async ({ page }) => {
  await prepare(page);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('这张收据会归入记录');
  await page.route('**/api/v1/order/list?**', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: {
          items: [
            {
              id: orderID,
              is_mine: true,
              nickname: '更新的昵称',
              avatar_url: '',
              amount_fen: 888,
              message: '更新后的留言',
              province: '上海',
              status: 'paid',
              date: '2026-10-30T08:01:00Z',
            },
          ],
          next_cursor: '',
        },
      },
    }),
  );
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('更新后的留言');
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('上海');
});

test('retrying a failed OAuth authorization preserves the payment return destination', async ({ page }) => {
  await prepare(page, { signedIn: false });
  const destinations: string[] = [];
  await page.route('**/api/v1/user/login/status', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: {
          stage: 'processing',
          provider: 'oidc',
          return_to: `/?payment=returned&order=${orderID}`,
        },
      },
    }),
  );
  await page.route('**/api/v1/user/login', (route) => {
    destinations.push(route.request().postDataJSON().return_to);
    return route.fulfill({
      json: { code: 'OK', data: { authorization_url: '/auth/complete?error=authorization' } },
    });
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await page.locator('.payment-return-status').getByRole('button', { name: '登录', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '登录', exact: true });
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect(page).toHaveURL('/auth/complete?error=authorization');
  await page.locator('main').getByRole('button', { name: '登录', exact: true }).click();
  await dialog.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect.poll(() => destinations.length).toBe(2);
  expect(destinations).toEqual([
    `/?payment=returned&order=${orderID}`,
    `/?payment=returned&order=${orderID}`,
  ]);
});
