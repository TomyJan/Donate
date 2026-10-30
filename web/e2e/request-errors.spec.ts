import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';

async function prepare(page: Page, signedIn = false) {
  await page.addInitScript(() => localStorage.setItem('donate:cookie-notice', '2026-10-30'));
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/user/profile') && !signedIn)
      return route.fulfill({ status: 401, json: { code: 'UNAUTHORIZED', data: null } });
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'test',
        providers: ['oidc'],
        version: { version: 'test', commit: '', tag: '' },
      };
    if (path.endsWith('/user/profile'))
      data = {
        id: 'request-test-user',
        nickname: 'Request Test',
        avatar_url: '',
        admin: false,
        bindings: [],
        csrf_token: 'test',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/order/list')) data = { items: [], next_cursor: '' };
    if (path.endsWith('/order/create')) data = { id: 'request-test-order' };
    await route.fulfill({ json: { code: 'OK', data } });
  });
}

test('guests can retry a failed record request without signing in', async ({ page }) => {
  await prepare(page);
  let recovered = false;
  await page.route('**/api/v1/order/list?**', async (route) => {
    if (!recovered) return route.fulfill({ status: 503, contentType: 'text/html', body: 'proxy busy' });
    await route.fulfill({ json: { code: 'OK', data: { items: [], next_cursor: '' } } });
  });
  await page.goto('/');
  const state = page.locator('.r-book .request-error');
  await expect(state).toContainText('服务器正在忙');
  recovered = true;
  await state.getByRole('button', { name: '重试', exact: true }).click();
  await expect(state).toHaveCount(0);
  await expect(page.locator('.r-book .r-empty')).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('a failed site request offers retry inside sign in instead of claiming no providers are configured', async ({
  page,
}, info) => {
  await prepare(page);
  let recovered = false;
  await page.route('**/api/v1/site/info', async (route) => {
    if (!recovered) return route.fulfill({ status: 502, contentType: 'text/plain', body: 'bad gateway' });
    await route.fulfill({
      json: {
        code: 'OK',
        data: {
          name: 'Donate',
          payment_enabled: true,
          providers: ['oidc'],
          version: { version: 'test', commit: '', tag: '' },
        },
      },
    });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.locator('.request-error')).toContainText('服务器正在忙');
  await page.screenshot({ path: info.outputPath('sign-in-retry-dark.png') });
  recovered = true;
  await dialog.getByRole('button', { name: '重试', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'OIDC', exact: true })).toBeVisible();
  await expect(dialog.locator('.request-error')).toHaveCount(0);
});

test('a rejected payment channel provides a specific action without discarding the order', async ({
  page,
}) => {
  await prepare(page, true);
  let created = 0;
  await page.route('**/api/v1/order/create', async (route) => {
    created++;
    await route.fulfill({ json: { code: 'OK', data: { id: 'request-test-order' } } });
  });
  await page.route('**/api/v1/order/payment/start', (route) =>
    route.fulfill({
      json: { code: 'PAYMENT_CHANNEL_UNAVAILABLE', message: 'secret upstream response', data: null },
    }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: '捐赠 · 支付宝', exact: true }).click();
  await expect(page.locator('.toast')).toContainText('换一种方式');
  await expect(page.locator('body')).not.toContainText('secret upstream response');
  expect(created).toBe(1);
  await expect(page.locator('#donation-amount')).toHaveValue('20.26');
});
