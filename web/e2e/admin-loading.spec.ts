import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';

async function holdAdmin(page: Page, stage: 'module' | 'identity' | 'overview' | 'orders') {
  let release = () => {};
  let held = false;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  if (stage === 'module')
    await page.route(/\/(?:src\/features\/Admin\.tsx|assets\/Admin-[^/]+\.js)(?:\?.*)?$/, async (route) => {
      held = true;
      await pending;
      await route.continue();
    });
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (
      (stage === 'identity' && path.endsWith('/user/profile')) ||
      (stage === 'overview' && path.endsWith('/admin/overview')) ||
      (stage === 'orders' && path.endsWith('/admin/order/list'))
    ) {
      held = true;
      await pending;
    }
    let data: unknown = null;
    if (path.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'csrf',
        providers: ['oidc'],
        version: { built_at: '', commit: '', tag: '', version: 'test' },
      };
    if (path.endsWith('/user/profile'))
      data = {
        id: 'administrator',
        admin: true,
        avatar_url: '',
        nickname: 'Admin',
        provider: 'oidc',
        bindings: [],
        csrf_token: 'csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/admin/overview'))
      data = { paid_fen: 100, paid: 1, pending: 0, closed: 0, daily: [], channels: [] };
    if (path.endsWith('/admin/order/list')) data = { items: [], total: 0, page: 1, page_size: 20 };
    await route.fulfill({ json: { code: 'OK', message: '', request_id: 'loading-test', data } });
  });
  await page.goto('/admin', { waitUntil: 'domcontentloaded' });
  return { release, isHeld: () => held };
}

for (const stage of ['module', 'identity'] as const)
  test(`${stage} loading uses the same responsive administration skeleton`, async ({ page }, info) => {
    const control = await holdAdmin(page, stage);
    try {
      await expect.poll(control.isHeld).toBe(true);
      const loading = page.getByRole('status', { name: '加载中', exact: true });
      await expect(loading).toBeVisible();
      await expect(loading.locator('.admin-loading-block')).not.toHaveCount(0);
      await expect(page.getByRole('button', { name: '订单', exact: true })).toHaveCount(0);
      for (const width of [320, 768, 1280]) {
        await page.setViewportSize({ width, height: 800 });
        await expect(loading).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.screenshot({ path: info.outputPath(`loading-${width}.png`), animations: 'disabled' });
      }
      await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
      await expect(loading.locator('.admin-loading-spinner')).toHaveCSS('animation-name', 'none');
      await expect(loading.locator('.admin-loading-preview')).toHaveCSS('animation-name', 'none');
      await expect(page.locator('.a-main')).toHaveCSS('transform', 'none');
      const start = await page.locator('.a-main').boundingBox();
      control.release();
      await expect(page.locator('.overview-stats')).toBeVisible();
      const ready = await page.locator('.a-main').boundingBox();
      expect(Math.abs((start?.x ?? 0) - (ready?.x ?? 0))).toBeLessThan(1);
      await expect
        .poll(async () => Math.abs((start?.y ?? 0) - ((await page.locator('.a-main').boundingBox())?.y ?? 0)))
        .toBeLessThan(1);
      await expect(loading).toHaveCount(0);
    } finally {
      control.release();
    }
  });

for (const stage of ['overview', 'orders'] as const)
  test(`${stage} initial data loads in place without leaving a bare text placeholder`, async ({ page }) => {
    const control = await holdAdmin(page, stage);
    try {
      if (stage === 'orders') {
        await page.locator('.a-side').getByRole('button', { name: '订单', exact: true }).click();
        await page.getByRole('textbox', { name: '昵称 / 订单 ID / 用户 ID', exact: true }).fill('保留筛选');
      }
      await expect.poll(control.isHeld).toBe(true);
      const loading = page.getByRole('status', { name: '加载中', exact: true });
      await expect(loading).toBeVisible();
      await expect(loading.locator('.admin-loading-block')).not.toHaveCount(0);
      await expect(page.locator('.a-side')).toBeVisible();
      control.release();
      await expect(loading).toHaveCount(0);
      if (stage === 'overview') await expect(page.locator('.overview-stats')).toBeVisible();
      else
        await expect(
          page.getByRole('textbox', { name: '昵称 / 订单 ID / 用户 ID', exact: true }),
        ).toHaveValue('保留筛选');
    } finally {
      control.release();
    }
  });
