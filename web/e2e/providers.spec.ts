import { expect, test } from '@playwright/test';

test('OAuth failure notices explain the next step without showing provider details', async ({ page }) => {
  for (const [code, notice] of [
    ['flow_expired', '授权已过期'],
    ['rate_limited', '操作太频繁'],
  ]) {
    await page.goto(`/auth/complete?error=${code}`);
    await expect(page.locator('.login-complete [role="alert"]')).toContainText(notice);
    await expect(page.locator('.login-complete')).not.toContainText('头像');
    await page.locator('.login-complete button').click();
    await expect(page.getByRole('dialog', { name: '登录', exact: true })).toBeVisible();
  }
});

test('QQ number verification can retry in place and distinguishes binding from sign-in', async ({ page }) => {
  await page.route('**/api/v1/user/login/status', (route) =>
    route.fulfill({
      json: { code: 'OK', data: { stage: 'qq_number', provider: 'qq', intent: 'bind', return_to: '/' } },
    }),
  );
  let attempts = 0;
  await page.route('**/api/v1/user/login/qq/complete', (route) => {
    attempts += 1;
    return route.fulfill({
      json: {
        code: attempts === 1 ? 'QQ_VERIFICATION_FAILED' : 'AUTH_ATTEMPTS_EXCEEDED',
        message: 'private-upstream-detail',
      },
    });
  });
  await page.goto('/auth/complete');
  await expect(page.locator('.login-complete')).toContainText('确认 QQ 号后完成绑定。');
  const number = page.getByRole('textbox', { name: 'QQ 号', exact: true });
  await number.fill('12345678');
  await page.locator('.d-qq-submit').click();
  await expect(page.getByRole('alert')).toContainText('QQ 号验证未通过');
  await expect(number).toHaveValue('12345678');
  await expect(page.locator('.login-complete')).not.toContainText('private-upstream-detail');
  await expect(page.locator('.login-complete')).not.toContainText('头像');
  await page.locator('.d-qq-submit').click();
  await expect(page.getByRole('alert')).toContainText('验证次数已用尽');
  await expect(number).toHaveCount(0);
  expect(attempts).toBe(2);
});

test('custom OIDC metadata labels sign-in and navigates using its public ID', async ({ page }) => {
  const site = await (await page.request.get('/api/v1/site/info')).json();
  const providerID = site.data.provider_details.find(
    (provider: { kind: string }) => provider.kind === 'oidc',
  ).id;
  await page.route('**/api/v1/site/info', async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.data.providers = [providerID, 'qq'];
    body.data.provider_details = [
      { id: providerID, kind: 'oidc', name: '我的通行证', icon_url: 'https://identity.test/icon.svg' },
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
  await page.goto('/');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '登录', exact: true });
  const provider = dialog.getByRole('button', { name: '我的通行证', exact: true });
  await expect(provider).toBeDisabled();
  await expect(provider.locator('img')).toHaveAttribute('src', 'https://identity.test/icon.svg');
  await dialog.getByRole('checkbox').check();
  const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/user/login');
  await provider.click();
  const login = await response;
  expect(login.status()).toBe(302);
  expect(login.request().method()).toBe('GET');
  expect(login.request().isNavigationRequest()).toBe(true);
  const query = new URL(login.url()).searchParams;
  expect(query.get('provider')).toBe(providerID);
  expect(query.get('return_to')).toBe('/');
  await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
});

test('an external IdP application link completes real GET login and honors return_to', async ({
  page,
  context,
  baseURL,
}) => {
  const origin = new URL(baseURL ?? '').origin;
  const site = await (await page.request.get('/api/v1/site/info')).json();
  const providerID = site.data.provider_details.find(
    (provider: { kind: string }) => provider.kind === 'oidc',
  ).id;
  const entry = new URL('/api/v1/user/login', origin);
  entry.searchParams.set('provider', providerID);
  entry.searchParams.set('return_to', '/admin');
  // Only the IdP application directory is synthetic. Every login, authorization,
  // token exchange and callback request goes through the running fixture.
  await page.route('https://idp.example.test/apps/donate', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: `<a href="${entry.href.replaceAll('&', '&amp;')}">Open Donate</a>`,
    }),
  );
  await page.goto('https://idp.example.test/apps/donate');
  await expect(page).toHaveURL('https://idp.example.test/apps/donate');
  expect(new URL(page.url()).origin).not.toBe(origin);
  const entryResponse = page.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/v1/user/login',
  );
  const callbackResponse = page.waitForResponse(
    (response) => new URL(response.url()).pathname === `/api/v1/user/oauth/callback/${providerID}`,
  );
  await page.getByRole('link', { name: 'Open Donate' }).click();
  const login = await entryResponse;
  expect(login.status()).toBe(302);
  expect(login.request().method()).toBe('GET');
  expect(login.request().isNavigationRequest()).toBe(true);
  expect(login.request().resourceType()).toBe('document');
  // Fetch Metadata can be absent on local HTTP requests. Header-based guards
  // have HTTP handler coverage; here verify the real cross-origin navigation.
  const authorization = new URL((await login.allHeaders()).location);
  expect(authorization.pathname).toBe('/authorize');
  expect(authorization.searchParams.get('state')).toBeTruthy();
  expect(authorization.searchParams.get('code_challenge_method')).toBe('S256');
  expect(authorization.searchParams.get('redirect_uri')).toBe(
    `${origin}/api/v1/user/oauth/callback/${providerID}`,
  );
  const flowHeader = (await login.headersArray()).find(
    (header) => header.name.toLowerCase() === 'set-cookie' && header.value.startsWith('donate_flow_dev='),
  )?.value;
  // The flow cookie is opaque; its internal ID/token layout is owned by auth.
  expect(flowHeader).toMatch(/^donate_flow_dev=[^;]+;/);
  expect(flowHeader).toContain('HttpOnly');
  expect(flowHeader).toContain('SameSite=Lax');
  const callback = await callbackResponse;
  expect(callback.status()).toBe(303);
  expect((await callback.allHeaders()).location).toBe('/admin');
  expect(new URL(callback.url()).searchParams.get('state')).toBe(authorization.searchParams.get('state'));
  await expect(page).toHaveURL(`${origin}/admin`);
  const profileResponse = await page.request.get('/api/v1/user/profile');
  expect(profileResponse.status()).toBe(200);
  const profile = await profileResponse.json();
  expect(profile.code).toBe('OK');
  expect(profile.data.nickname).toBe('Fixture Admin');
  expect(profile.data.provider).toBe('oidc');
  expect(profile.data.admin).toBe(true);
  expect(profile.data.csrf_token).toBeTruthy();
  expect(
    (await context.cookies(origin)).some(
      (cookie) => cookie.name.startsWith('donate_session_dev_') && cookie.value !== '',
    ),
  ).toBe(true);
});
