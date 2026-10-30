import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';

async function openOrders(page: Page, lang: 'en' | 'zh-CN' = 'en', theme = 'light') {
  const requests: URL[] = [];
  await page.addInitScript(
    ({ lang, theme }) => {
      localStorage.setItem('donate-language', lang);
      localStorage.setItem('donate-theme', theme);
    },
    { lang, theme },
  );
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    let data: unknown = {};
    if (url.pathname.endsWith('/site/info'))
      data = {
        name: 'Donate',
        logo: '',
        payment_enabled: true,
        csrf_token: 'csrf',
        providers: ['oidc'],
        version: { built_at: '', commit: '', tag: '', version: 'test' },
      };
    if (url.pathname.endsWith('/user/profile'))
      data = {
        id: 'administrator',
        admin: true,
        avatar_url: '',
        nickname: 'Administrator',
        provider: 'oidc',
        bindings: [],
        csrf_token: 'csrf',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (url.pathname.endsWith('/admin/overview'))
      data = { paid_fen: 0, paid: 0, pending: 0, closed: 0, daily: [], channels: [] };
    if (url.pathname.endsWith('/admin/order/list')) {
      requests.push(url);
      data = { items: [], total: 0, page: 1, page_size: 20 };
    }
    if (url.pathname.endsWith('/admin/user/list')) data = { items: [], total: 0, page: 1, page_size: 20 };
    await route.fulfill({ json: { code: 'OK', message: '', request_id: 'calendar-filters', data } });
  });
  await page.goto('/admin');
  await page
    .locator('.a-side')
    .getByRole('button', { name: lang === 'en' ? 'Orders' : '订单', exact: true })
    .click();
  await page.getByRole('button', { name: lang === 'en' ? 'Filters' : '筛选', exact: true }).click();
  await expect(page.locator('.a-filters')).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  return requests;
}

for (const lang of ['en', 'zh-CN'] as const)
  for (const theme of ['light', 'dark']) {
    test(`${lang} ${theme}: maximum and visibility stay aligned with capped fields`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width: 1280, height: 900 });
      await openOrders(page, lang, theme);
      const maximum = page.getByRole('textbox', {
        name: lang === 'en' ? 'Maximum' : '最高金额',
        exact: true,
      });
      const visibility = page.getByRole('combobox', {
        name: lang === 'en' ? 'Visibility' : '公开范围',
        exact: true,
      });
      for (const width of [1280, 768, 320]) {
        await page.setViewportSize({ width, height: 900 });
        await expect
          .poll(async () => {
            const max = await maximum.boundingBox(),
              visible = await visibility.boundingBox();
            return max && visible ? Math.abs(max.y - visible.y) : 999;
          })
          .toBeLessThan(1);
        const max = await maximum.boundingBox(),
          visible = await visibility.boundingBox();
        expect(max?.height).toBe(visible?.height);
        const fields = await page
          .locator('.a-filters .a-field')
          .evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().width));
        expect(Math.max(...fields)).toBeLessThanOrEqual(220);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
          true,
        );
        await page.screenshot({ path: info.outputPath(`filters-${width}.png`), animations: 'disabled' });
      }
      await page.getByRole('button', { name: lang === 'en' ? 'From' : '开始日期', exact: true }).click();
      await expect(
        page.getByRole('combobox', { name: lang === 'en' ? 'Choose year' : '选择年份', exact: true }),
      ).toBeVisible();
      const captionRows = await page
        .locator('.date-calendar-heading button')
        .evaluateAll((buttons) => buttons.map((button) => Math.round(button.getBoundingClientRect().y)));
      expect(new Set(captionRows).size).toBe(1);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath('calendar-320.png'), animations: 'disabled' });
    });
  }

test('calendar title controls share the arrow row and preserve Shanghai time through keyboard browsing', async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' });
  const requests = await openOrders(page, 'en', 'dark');
  const from = page.getByRole('button', { name: 'From', exact: true });
  await from.click();
  const caption = page.locator('.date-calendar-heading');
  await expect(caption).toBeVisible();
  const bounds = await caption.locator('button').evaluateAll((elements) =>
    elements.map((element) => ({
      y: element.getBoundingClientRect().y,
      height: element.getBoundingClientRect().height,
    })),
  );
  expect(new Set(bounds.map(({ y }) => Math.round(y))).size).toBe(1);
  expect(bounds).toHaveLength(4);
  await page.getByRole('combobox', { name: 'Choose year', exact: true }).click();
  await page.getByRole('option', { name: '2024', exact: true }).click();
  const month = page.getByRole('combobox', { name: 'Choose month', exact: true });
  await month.focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.locator('[role="option"][data-state="checked"]')).toBeFocused();
  await page.keyboard.press('Home');
  await expect(page.getByRole('option', { name: 'January', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('grid')).toHaveAttribute('aria-label', 'January 2024');
  await page.locator('[data-day="2024-01-31"] button').click();
  for (const [label, value] of [
    ['Hour', '23'],
    ['Minute', '47'],
  ]) {
    await page.getByRole('combobox', { name: label, exact: true }).click();
    await page.getByRole('option', { name: value, exact: true }).click();
  }
  await page.getByRole('combobox', { name: 'Choose year', exact: true }).click();
  await page.getByRole('option', { name: '2025', exact: true }).click();
  await month.focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.locator('[role="option"][data-state="checked"]')).toBeFocused();
  await page.keyboard.press('Home');
  await expect(page.getByRole('option', { name: 'January', exact: true })).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('option', { name: 'February', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('grid')).toHaveAttribute('aria-label', 'February 2025');
  await expect(from).toContainText('1/31/2024 23:47');
  await page.screenshot({ path: info.outputPath('calendar-320-dark.png'), animations: 'disabled' });
  await page.locator('[data-day="2025-02-28"] button').click();
  await expect(from).toContainText('2/28/2025 23:47');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'Confirm', exact: true }).click();
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect.poll(() => requests.at(-1)?.searchParams.get('from')).toBe('2025-02-28T15:47:00.000Z');
});
