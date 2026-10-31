import { expect, test } from '@playwright/test';
import { LEGAL_VERSION } from '../src/lib/legal';

test('agreement has a generous hit area and centered tick in both themes', async ({ page }, info) => {
  await page.addInitScript((version) => localStorage.setItem('donate:cookie-notice', version), LEGAL_VERSION);
  await page.route('**/api/v1/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/user/profile'))
      return route.fulfill({ status: 401, json: { code: 'UNAUTHORIZED', data: null } });
    return route.fulfill({
      json: {
        code: 'OK',
        data: path.endsWith('/site/info')
          ? { providers: ['oidc'], payment_enabled: true, version: { version: 'test', commit: '', tag: '' } }
          : { items: [], next_cursor: '' },
      },
    });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme });
    await page.goto('/');
    await page.getByRole('button', { name: '登录', exact: true }).click();
    const dialog = page.getByRole('dialog');
    const checkbox = dialog.getByRole('checkbox');
    await checkbox.uncheck();
    await checkbox.focus();
    await page.keyboard.press('Space');
    await expect(checkbox).toBeChecked();
    await expect(dialog.locator('.agreement-check svg')).toHaveCSS('opacity', '1');
    await expect(checkbox).toHaveCSS('outline-style', 'none');
    await expect(dialog.getByRole('button', { name: 'OIDC', exact: true })).toBeEnabled();
    await expect
      .poll(async () => {
        const hit = await checkbox.boundingBox();
        const tick = await dialog.locator('.agreement-check svg').boundingBox();
        if (!hit || !tick) return false;
        return (
          hit.width >= 24 &&
          hit.height >= 24 &&
          tick.width <= 14 &&
          tick.height <= 14 &&
          Math.abs(tick.x + tick.width / 2 - hit.x - hit.width / 2) <= 1 &&
          Math.abs(tick.y + tick.height / 2 - hit.y - hit.height / 2) <= 1
        );
      })
      .toBe(true);
    await page.screenshot({ path: info.outputPath(`agreement-${colorScheme}.png`) });
    await page.reload();
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await expect(checkbox).toBeChecked();
    await checkbox.uncheck();
  }
});
