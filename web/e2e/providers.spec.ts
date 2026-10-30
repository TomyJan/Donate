import { expect, test } from '@playwright/test';

test('custom OIDC metadata labels sign-in and submits its public ID', async ({ page }) => {
  await page.route('**/api/v1/site/info', async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.data.providers = ['campus', 'qq'];
    body.data.provider_details = [
      { id: 'campus', kind: 'oidc', name: '我的通行证', icon_url: 'https://identity.test/icon.svg' },
      { id: 'qq', kind: 'qq', name: 'QQ', icon_url: '' },
    ];
    await route.fulfill({ json: body });
  });
  await page.route('https://identity.test/icon.svg', (route) =>
    route.fulfill({
      contentType: 'image/svg+xml',
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24"><circle cx="12" cy="12" r="10" fill="coral"/></svg>',
    }),
  );
  await page.route('**/api/v1/user/login', (route) =>
    route.fulfill({ json: { code: 'OK', data: { authorization_url: '/provider-test' } } }),
  );
  await page.route('**/provider-test', (route) =>
    route.fulfill({ contentType: 'text/html', body: '<h1>Provider sign-in</h1>' }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '登录', exact: true });
  const provider = dialog.getByRole('button', { name: '我的通行证', exact: true });
  await expect(provider).toBeDisabled();
  await expect(provider.locator('img')).toHaveAttribute('src', 'https://identity.test/icon.svg');
  await dialog.getByRole('checkbox').check();
  const request = page.waitForRequest((r) => r.url().endsWith('/api/v1/user/login') && r.method() === 'POST');
  await provider.click();
  expect((await request).postDataJSON().provider).toBe('campus');
  await expect(page.getByRole('heading', { name: 'Provider sign-in' })).toBeVisible();
});
