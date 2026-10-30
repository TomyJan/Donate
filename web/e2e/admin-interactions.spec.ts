import type { Locator } from '@playwright/test';
import { expect, test } from '@playwright/test';
import { words } from '../src/lib/i18n';

async function fitsViewport(surface: Locator) {
  await expect(surface).toBeVisible();
  await expect(surface).toHaveCSS('opacity', '1');
  expect(
    await surface.evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return el.scrollWidth <= el.clientWidth + 1 && rect.left >= 0 && rect.right <= innerWidth;
    }),
  ).toBe(true);
}

for (const [width, english, dark] of [
  [320, false, false],
  [320, true, true],
  [390, false, true],
  [768, true, true],
  [900, true, false],
] as const) {
  test(
    'admin interactions at ' +
      width +
      'px, ' +
      (english ? 'English' : 'Chinese') +
      ', ' +
      (dark ? 'dark' : 'light'),
    async ({ page }) => {
      const t = (key: keyof typeof words) => words[key][english ? 1 : 0];
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/');
      await page.getByRole('button', { name: '登录', exact: true }).click();
      await page.getByRole('button', { name: 'OIDC', exact: true }).click();
      await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
      await page.getByRole('button', { name: '切换外观', exact: true }).click();
      await page.getByRole('menuitem', { name: dark ? '深色' : '浅色', exact: true }).click();
      if (english) {
        await page.getByRole('button', { name: '切换语言', exact: true }).click();
        await page.getByRole('menuitem', { name: 'English', exact: true }).click();
      }
      await page.goto('/admin');
      const navigation = page.getByRole('navigation', { name: t('admin'), exact: true });
      await navigation.getByRole('button', { name: t('orders'), exact: true }).click();
      await page.getByRole('combobox', { name: t('status'), exact: true }).click();
      await fitsViewport(page.getByRole('listbox'));
      await page.getByRole('option', { name: t('paid'), exact: true }).click();
      await page.getByRole('button', { name: t('filters'), exact: true }).click();
      await page.getByRole('button', { name: t('from'), exact: true }).click();
      await fitsViewport(page.locator('.date-pop'));
      await page.getByRole('combobox', { name: 'Hour', exact: true }).click();
      await fitsViewport(page.getByRole('listbox'));
      await page.getByRole('option', { name: '08', exact: true }).click();
      await page
        .locator('.date-pop')
        .getByRole('button', { name: t('confirm'), exact: true })
        .click();
      await page.getByRole('button', { name: t('reset'), exact: true }).click();
      await page.getByRole('button', { name: t('filters'), exact: true }).click();
      await page.locator('th[data-field=amount_fen] button').click();
      await expect(page.locator('th[data-field=amount_fen]')).toHaveAttribute('aria-sort', 'descending');
      await page.locator('tbody .custom-check').first().check();
      await page.getByRole('button', { name: t('batch'), exact: true }).click();
      await fitsViewport(page.getByRole('menu'));
      await page.getByRole('menuitem', { name: t('hide_message'), exact: true }).click();
      await fitsViewport(page.getByRole('dialog'));
      await page
        .getByRole('dialog')
        .getByRole('button', { name: t('cancel'), exact: true })
        .click();
      await page
        .getByRole('button', { name: new RegExp(`^${t('details')} `) })
        .first()
        .click();
      const detail = page
        .getByRole('dialog')
        .filter({ has: page.getByRole('heading', { name: t('details'), exact: true }) });
      await fitsViewport(detail);
      await detail.getByRole('button', { name: t('reconcile'), exact: true }).click();
      await expect(page.locator('.toast').filter({ hasText: t('queued') })).toBeVisible();
      await detail.getByRole('button', { name: t('delete'), exact: true }).click();
      const confirmation = page.getByRole('dialog', { name: t('delete'), exact: true });
      await fitsViewport(confirmation);
      await confirmation.getByRole('button', { name: t('cancel'), exact: true }).click();
      await detail
        .getByRole('button', { name: t('close'), exact: true })
        .first()
        .click();
      await navigation.getByRole('button', { name: t('users'), exact: true }).click();
      await page
        .getByRole('button', { name: new RegExp(`^${t('userDetail')} `) })
        .first()
        .click();
      await fitsViewport(page.getByRole('dialog'));
      await page.screenshot({
        path: `test-results/admin-user-${width}-${english ? 'en' : 'zh'}.png`,
        fullPage: true,
      });
      await page
        .getByRole('dialog')
        .getByRole('button', { name: t('close'), exact: true })
        .first()
        .click();
      await page.getByRole('button', { name: t('account'), exact: true }).click();
      await fitsViewport(page.getByRole('menu'));
      await page.getByRole('menuitem', { name: t('account'), exact: true }).click();
      await fitsViewport(page.getByRole('dialog'));
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      expect(errors).toEqual([]);
    },
  );
}
