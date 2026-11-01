import { createHmac } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { LEGAL_VERSION } from '../src/lib/legal';

test('cancels a stale site response before it can replace the anonymous sign-in cookie', async ({
  page,
  context,
  baseURL,
}) => {
  const origin = new URL(baseURL ?? '').origin;
  const discovery = await (await page.request.get('/api/v1/site/info')).json();
  const providerID = discovery.data.provider_details.find(
    (provider: { kind: string }) => provider.kind === 'oidc',
  ).id;
  const csrf = (token: string) =>
    createHmac('sha256', Buffer.alloc(32, 7)).update(`anonymous-csrf:${token}`).digest('base64url');
  const oldCookie = 'a'.repeat(43);
  const newCookie = 'b'.repeat(43);
  const profile = {
    id: '00000000-0000-4000-8000-000000000001',
    nickname: 'Synthetic user',
    csrf_token: 'c'.repeat(43),
    expires_at: new Date(Date.now() + 19 * 86400000).toISOString(),
    avatar_url: '',
    provider: 'oidc',
    admin: false,
    bindings: [],
  };
  const site = {
    logo: '',
    name: 'Synthetic site',
    providers: [providerID],
    provider_details: [{ id: providerID, kind: 'oidc', name: 'Synthetic OIDC', icon_url: '' }],
    payment_enabled: true,
    version: { version: '0.0.4', tag: '', commit: 'synthetic', built_at: '' },
  };
  let sites = 0;
  let profiles = 0;
  let releaseOld = () => {};
  let finishOld = () => {};
  const oldGate = new Promise<void>((resolve) => {
    releaseOld = resolve;
  });
  const oldFinished = new Promise<void>((resolve) => {
    finishOld = resolve;
  });
  await page.addInitScript((version) => {
    localStorage.setItem('donate:legal-consent', version);
    localStorage.setItem('donate:cookie-notice', version);
  }, LEGAL_VERSION);
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      // Real local fixture authorization runs on its own origin.
      if (url.hostname === '127.0.0.1' || url.hostname === 'localhost') await route.continue();
      else await route.abort();
      return;
    }
    if (!url.pathname.startsWith('/api/v1/')) {
      await route.continue();
      return;
    }
    if (url.pathname.endsWith('/site/info')) {
      const first = ++sites === 1;
      if (first) await oldGate;
      try {
        await route.fulfill({
          json: { code: 'OK', data: { ...site, csrf_token: first ? profile.csrf_token : csrf(newCookie) } },
          headers: {
            'set-cookie': `donate_csrf_dev=${first ? oldCookie : newCookie}; Path=/; HttpOnly; SameSite=Lax`,
          },
        });
      } finally {
        if (first) finishOld();
      }
      return;
    }
    if (url.pathname.endsWith('/user/profile')) {
      const first = ++profiles === 1;
      await route.fulfill({
        status: first ? 200 : 401,
        json: { code: first ? 'OK' : 'UNAUTHORIZED', data: first ? profile : null },
      });
      return;
    }
    if (
      url.pathname.endsWith('/user/login') ||
      url.pathname === `/api/v1/user/oauth/callback/${providerID}`
    ) {
      await route.continue();
      return;
    }
    if (url.pathname.endsWith('/order/list')) {
      await route.fulfill({ json: { code: 'OK', data: { items: [], next_cursor: '' } } });
      return;
    }
    throw new Error(`Unexpected synthetic API: ${url.pathname}`);
  });
  try {
    await page.goto('/', { waitUntil: 'domcontentloaded' });
    await expect.poll(() => sites).toBe(2);
    await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible();
    await expect
      .poll(async () => (await context.cookies()).find((cookie) => cookie.name === 'donate_csrf_dev')?.value)
      .toBe(newCookie);
    releaseOld();
    await oldFinished;
    expect((await context.cookies()).find((cookie) => cookie.name === 'donate_csrf_dev')?.value).toBe(
      newCookie,
    );
    await page.getByRole('button', { name: '登录', exact: true }).click();
    const response = page.waitForResponse((value) => new URL(value.url()).pathname === '/api/v1/user/login');
    const callback = page.waitForResponse(
      (value) => new URL(value.url()).pathname === `/api/v1/user/oauth/callback/${providerID}`,
    );
    await page.getByRole('button', { name: 'Synthetic OIDC', exact: true }).click();
    const login = await response;
    expect(login.status()).toBe(302);
    expect(login.request().method()).toBe('GET');
    expect(login.request().isNavigationRequest()).toBe(true);
    expect(new URL(login.url()).searchParams.get('provider')).toBe(providerID);
    expect((await login.request().allHeaders())['x-csrf-token']).toBeUndefined();
    expect((await callback).status()).toBe(303);
    await expect(page).toHaveURL(`${origin}/`);
    const current = await (await page.request.get('/api/v1/user/profile')).json();
    expect(current.code).toBe('OK');
    expect(current.data.nickname).toBe('Fixture Admin');
  } finally {
    releaseOld();
    await page.unrouteAll({ behavior: 'wait' });
  }
});
