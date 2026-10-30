import { expect, test } from '@playwright/test';

test('an expired administrator session shows a responsive sign-in card and can sign in again', async ({
  page,
}) => {
  let expired = false;
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (expired && (path.endsWith('/user/profile') || path.endsWith('/admin/overview'))) {
      await route.fulfill({ status: 401, json: { code: 'UNAUTHORIZED', message: '', data: null } });
      return;
    }
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'csrf',
        providers: ['oidc'],
        version: { version: 'test', commit: '', tag: '', built_at: '' },
      };
    if (path.endsWith('/user/profile'))
      data = {
        id: 'administrator',
        nickname: 'Admin',
        avatar_url: '',
        provider: 'oidc',
        admin: true,
        bindings: [],
        csrf_token: 'csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/admin/overview'))
      data = { paid_fen: 100, paid: 1, pending: 0, closed: 0, daily: [], channels: [] };
    await route.fulfill({ json: { code: 'OK', data } });
  });
  await page.goto('/admin');
  await expect(page.locator('.overview-stats')).toBeVisible();
  expired = true;
  await page.locator('.overview-refresh').click();
  const card = page.locator('.access-card');
  await expect(card.getByRole('heading', { name: '请先登录', exact: true })).toBeVisible();
  await expect(page.locator('.overview-stats')).toHaveCount(0);
  for (const width of [320, 768, 1280]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(card).toBeVisible();
    expect(
      await card.evaluate((el) => {
        const r = el.getBoundingClientRect();
        return r.left >= 0 && r.right <= innerWidth && el.scrollWidth <= el.clientWidth;
      }),
    ).toBe(true);
    const buttons = await card
      .locator('.access-actions > *')
      .evaluateAll((els) => els.map((el) => el.getBoundingClientRect().top));
    expect(Math.max(...buttons) - Math.min(...buttons)).toBeLessThan(1);
    await page.screenshot({ path: `test-results/access-expired-${width}.png` });
  }
  await card.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '登录', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'OIDC', exact: true })).toBeVisible();
});

test('revoking admin permission replaces stale metrics while keeping the user signed in', async ({
  page,
}) => {
  let failure = '';
  let profiles = 0;
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/admin/overview') && failure) {
      await route.fulfill({ status: 403, json: { code: failure, data: null } });
      return;
    }
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        providers: ['oidc'],
        version: { version: 'test', commit: '', tag: '', built_at: '' },
      };
    if (path.endsWith('/user/profile')) {
      profiles++;
      data = {
        id: 'administrator',
        nickname: 'Admin',
        admin: failure !== 'FORBIDDEN',
        bindings: [],
        csrf_token: 'csrf',
      };
    }
    if (path.endsWith('/admin/overview'))
      data = { paid_fen: 100, paid: 1, pending: 0, closed: 0, daily: [], channels: [] };
    await route.fulfill({ json: { code: 'OK', data } });
  });
  await page.goto('/admin');
  await expect(page.locator('.overview-stats')).toBeVisible();
  const initialProfiles = profiles;
  failure = 'CSRF_FAILED';
  await page.locator('.overview-refresh').click();
  await expect(page.locator('.toast')).toBeVisible();
  await expect(page.locator('.overview-refresh')).toBeEnabled();
  await expect(page.locator('.overview-stats')).toBeVisible();
  expect(profiles).toBe(initialProfiles);
  failure = 'FORBIDDEN';
  await page.locator('.overview-refresh').click();
  await expect(
    page.locator('.access-card').getByRole('heading', { name: '无权访问', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.overview-stats')).toHaveCount(0);
  await expect(page.locator('.access-card').getByRole('button', { name: '登录', exact: true })).toHaveCount(
    0,
  );
  await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
  await expect.poll(() => profiles).toBeGreaterThan(initialProfiles);
});
