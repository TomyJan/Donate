import { createHmac } from 'node:crypto';
import { expect, test } from '@playwright/test';

test('cancels a stale site response before it can replace the anonymous sign-in cookie', async ({
  page,
  context,
  baseURL,
}) => {
  const origin = new URL(baseURL ?? '').origin;
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
    providers: ['oidc'],
    provider_details: [{ id: 'oidc', kind: 'oidc', name: 'Synthetic OIDC', icon_url: '' }],
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
  await page.addInitScript(() => {
    localStorage.setItem('donate:legal-consent', '2026-10-30');
    localStorage.setItem('donate:cookie-notice', '2026-10-30');
  });
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      await route.abort();
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
    if (url.pathname.endsWith('/user/login')) {
      const headers = await route.request().allHeaders();
      const token = /(?:^|; )donate_csrf_dev=([^;]+)/.exec(headers.cookie ?? '')?.[1] ?? '';
      const valid = token === newCookie && headers['x-csrf-token'] === csrf(token);
      await route.fulfill({
        status: valid ? 200 : 403,
        json: {
          code: valid ? 'OK' : 'CSRF_FAILED',
          data: valid ? { authorization_url: '/#authorized' } : null,
        },
      });
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
    const response = page.waitForResponse((value) => value.url().endsWith('/api/v1/user/login'));
    await page.getByRole('button', { name: 'Synthetic OIDC', exact: true }).click();
    expect((await response).status()).toBe(200);
    await expect(page).toHaveURL(/#authorized$/);
  } finally {
    releaseOld();
    await page.unrouteAll({ behavior: 'wait' });
  }
});
