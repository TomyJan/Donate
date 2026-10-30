import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';

async function prepareShell(page: Page, signedIn = false, admin = false) {
  await page.addInitScript(() => {
    localStorage.setItem('donate:cookie-notice', '2026-10-30');
  });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'shell-csrf',
        providers: ['oidc'],
        version: { built_at: '', commit: '', tag: '', version: 'test' },
      };
    if (path.endsWith('/user/profile')) {
      if (!signedIn) {
        await route.fulfill({ status: 401, json: { code: 'UNAUTHORIZED', data: null } });
        return;
      }
      data = {
        id: 'shell-user',
        nickname: 'Shell',
        avatar_url: '',
        provider: 'oidc',
        admin,
        bindings: [],
        csrf_token: 'shell-csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    }
    if (path.endsWith('/order/list')) data = { items: [], next_cursor: '' };
    if (path.endsWith('/admin/overview'))
      data = { paid_fen: 0, paid: 0, pending: 0, closed: 0, daily: [], channels: [] };
    await route.fulfill({ json: { code: 'OK', message: '', request_id: 'shell-test', data } });
  });
}

for (const state of [
  { name: 'guest home', path: '/', signedIn: false, admin: false, content: '.r-empty' },
  { name: 'signed-in home', path: '/', signedIn: true, admin: false, content: '.r-empty' },
  {
    name: 'login completion',
    path: '/auth/complete?error=login',
    signedIn: false,
    admin: false,
    content: '.login-complete',
  },
  { name: 'admin sign-in', path: '/admin', signedIn: false, admin: false, content: '.access-card' },
  { name: 'admin forbidden', path: '/admin', signedIn: true, admin: false, content: '.access-card' },
  { name: 'admin overview', path: '/admin', signedIn: true, admin: true, content: '.overview-stats' },
])
  test(`${state.name} keeps the footer at the bottom of a short page`, async ({ page }, info) => {
    await prepareShell(page, state.signedIn, state.admin);
    await page.setViewportSize({ width: 1280, height: 1200 });
    await page.goto(state.path);
    await expect(page.locator(state.content)).toBeVisible();
    for (const viewport of [
      { width: 1280, height: 1200 },
      { width: 390, height: 1100 },
    ]) {
      await page.setViewportSize(viewport);
      await expect
        .poll(() =>
          page
            .locator('.d-footer')
            .evaluate((el) => Math.abs(el.getBoundingClientRect().bottom - innerHeight)),
        )
        .toBeLessThanOrEqual(1);
      const layout = await page.locator('.d-footer').evaluate((el) => ({
        top: el.getBoundingClientRect().top,
        contentBottom:
          document.querySelector('main, .access-page, .a-layout')?.getBoundingClientRect().bottom ?? 0,
        overflow: document.documentElement.scrollWidth > innerWidth,
        position: getComputedStyle(el).position,
      }));
      expect(layout.top).toBeGreaterThanOrEqual(layout.contentBottom - 1);
      expect(layout.overflow).toBe(false);
      expect(['fixed', 'absolute']).not.toContain(layout.position);
      if (state.name === 'guest home')
        await page.screenshot({
          path: info.outputPath(`short-home-${viewport.width}.png`),
          animations: 'disabled',
        });
    }
  });

test('all page shells share the canvas color through explicit and system appearance changes', async ({
  page,
}) => {
  await prepareShell(page, true, true);
  for (const path of ['/', '/admin', '/auth/complete?error=login', '/terms']) {
    await page.goto(path);
    for (const theme of [
      { name: '浅色', system: 'dark', background: 'rgb(236, 235, 231)' },
      { name: '深色', system: 'light', background: 'rgb(33, 34, 31)' },
      { name: '跟随系统', system: 'dark', background: 'rgb(33, 34, 31)' },
      { name: '跟随系统', system: 'light', background: 'rgb(236, 235, 231)' },
    ] as const) {
      await page.emulateMedia({ colorScheme: theme.system });
      await page.getByRole('button', { name: '切换外观', exact: true }).click();
      await page.getByRole('menuitem', { name: theme.name, exact: true }).click();
      await expect(page.locator('html')).toHaveCSS('background-color', theme.background);
      await expect(page.locator('.d-app')).toHaveCSS('background-color', theme.background);
    }
  }
});

test('a long page keeps the footer after its content as the document scrolls', async ({ page }, info) => {
  await prepareShell(page);
  await page.setViewportSize({ width: 390, height: 720 });
  await page.goto('/terms');
  await expect(page.locator('.legal-paper')).toBeVisible();
  const footer = page.locator('.d-footer');
  const initial = await footer.boundingBox();
  expect(initial?.y).toBeGreaterThan(720);
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect(footer).toBeInViewport();
  const metrics = await page.evaluate(() => ({
    contentBottom: document.querySelector('.legal-main')?.getBoundingClientRect().bottom ?? 0,
    footerTop: document.querySelector('.d-footer')?.getBoundingClientRect().top ?? 0,
    footerBottom: document.querySelector('.d-footer')?.getBoundingClientRect().bottom ?? 0,
    viewportHeight: innerHeight,
  }));
  expect(metrics.footerTop).toBeGreaterThanOrEqual(metrics.contentBottom - 1);
  expect(Math.abs(metrics.footerBottom - metrics.viewportHeight)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: info.outputPath('long-page-footer.png'), animations: 'disabled' });
  await expect(page.locator('.d-top')).toHaveAttribute('data-hidden', 'true');
  await page.evaluate(() => window.scrollBy(0, -40));
  await page.getByRole('button', { name: '回到顶部', exact: true }).click();
  await expect.poll(() => page.evaluate(() => scrollY)).toBe(0);
  await expect(page.locator('.d-top')).toHaveAttribute('data-hidden', 'false');
});
