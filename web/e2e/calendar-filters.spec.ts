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
    test(`${lang} ${theme}: filters fill each row while maximum and visibility stay aligned`, async ({
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
        const trailingGaps = await page.locator('.a-filters').evaluate((panel) => {
          const right =
            panel.getBoundingClientRect().right - Number.parseFloat(getComputedStyle(panel).paddingRight);
          const rows = new Map<number, number>();
          for (const field of panel.querySelectorAll('.a-field')) {
            const box = field.getBoundingClientRect();
            const y = Math.round(box.top);
            rows.set(y, Math.max(rows.get(y) ?? 0, box.right));
          }
          return [...rows.values()].map((edge) => right - edge);
        });
        expect(Math.max(...trailingGaps)).toBeLessThan(2);
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
      const captionBounds = await page.locator('.date-calendar-heading button').evaluateAll((buttons) =>
        buttons.map((button) => ({
          left: button.getBoundingClientRect().left,
          right: button.getBoundingClientRect().right,
        })),
      );
      for (let index = 1; index < captionBounds.length; index++)
        expect(captionBounds[index].left - captionBounds[index - 1].right).toBeGreaterThanOrEqual(4);
      for (const control of ['.date-calendar-year', '.date-calendar-month'])
        await expect(page.locator(control)).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath('calendar-320.png'), animations: 'disabled' });
    });
  }

for (const theme of ['light', 'dark']) {
  test(`${theme}: modal close targets remain fully visible and clickable`, async ({ page }) => {
    await openOrders(page, 'en', theme);
    for (const width of [320, 768, 1280]) {
      await page.setViewportSize({ width, height: 900 });
      await page.locator('.d-nav').getByRole('button', { name: 'Account', exact: true }).click();
      await page.getByRole('menuitem', { name: 'Account', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Account', exact: true });
      const close = dialog.getByRole('button', { name: 'Close', exact: true });
      await close.focus();
      await expect(close).toBeFocused();
      await expect
        .poll(() =>
          close.evaluate((button) => {
            const box = button.getBoundingClientRect();
            return [
              [box.left + box.width / 2, box.top + 3],
              [box.right - 3, box.top + box.height / 2],
            ].every(([x, y]) => button.contains(document.elementFromPoint(x, y)));
          }),
        )
        .toBe(true);
      const box = await close.boundingBox();
      if (!box) throw new Error('Close button has no hit target');
      await page.mouse.click(box.x + box.width / 2, box.y + 3);
      await expect(dialog).toHaveCount(0);
    }
  });

  test(`${theme}: menus dialogs date pickers and nested selects have opaque elevated surfaces`, async ({
    page,
  }) => {
    await openOrders(page, 'en', theme);
    const elevated = async (surface: ReturnType<Page['locator']>) => {
      await expect(surface).toBeVisible();
      await expect(surface).not.toHaveCSS('box-shadow', 'none');
      await expect(surface).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
      await expect(surface).toHaveCSS('opacity', '1');
    };
    for (const label of ['Language', 'Appearance', 'Account']) {
      await page.locator('.d-nav').getByRole('button', { name: label, exact: true }).click();
      await elevated(page.getByRole('menu'));
      await page.keyboard.press('Escape');
    }
    await page.getByRole('combobox', { name: 'Status', exact: true }).click();
    await elevated(page.getByRole('listbox'));
    await page.keyboard.press('Escape');
    await page.locator('.d-nav').getByRole('button', { name: 'Account', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Account', exact: true }).click();
    await elevated(page.getByRole('dialog', { name: 'Account', exact: true }));
    await page.getByRole('button', { name: 'Close', exact: true }).click();
    await page.setViewportSize({ width: 320, height: 900 });
    await page.getByRole('button', { name: 'From', exact: true }).click();
    await elevated(page.locator('.date-time-pop'));
    for (const label of ['Choose year', 'Choose month', 'Hour', 'Minute']) {
      await page.getByRole('combobox', { name: label, exact: true }).click();
      await elevated(page.getByRole('listbox'));
      await page.keyboard.press('Escape');
      await expect(page.locator('.date-time-pop')).toBeVisible();
    }
    await page.keyboard.press('Escape');
    await page.locator('.a-side').getByRole('button', { name: 'Overview', exact: true }).click();
    await page.getByRole('button', { name: 'Custom', exact: true }).click();
    await elevated(page.locator('.range-pop'));
    await page.getByRole('button', { name: 'From', exact: true }).click();
    await elevated(page.locator('.date-time-pop'));
    await expect(page.locator('.range-pop')).toBeVisible();
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
  for (const width of [1920, 1512, 1280, 1080, 900, 768, 680, 640, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(from).toBeVisible();
    const fit = await from.evaluate((button) => {
      const text = button.querySelector('span');
      const field = button.closest('.a-field-date');
      return {
        width: field?.getBoundingClientRect().width ?? 0,
        panelWidth: field?.parentElement?.clientWidth ?? 0,
        textFits: !!text && text.scrollWidth <= text.clientWidth + 1,
        singleLine: !!text && text.getBoundingClientRect().height < 30,
      };
    });
    expect(fit.width).toBeGreaterThanOrEqual(Math.min(240, fit.panelWidth - 28));
    expect(fit.textFits).toBe(true);
    expect(fit.singleLine).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect.poll(() => requests.at(-1)?.searchParams.get('from')).toBe('2025-02-28T15:47:00.000Z');
});
