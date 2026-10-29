import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';

async function login(page: Page) {
  await page.goto('/');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
}
test('guest scope, input, preferences and responsive shared header', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.r-index-row')).toHaveCount(10);
  const list = await (await page.request.get('/api/v1/order/list?limit=999&cursor=anything')).json();
  expect(list.data.items).toHaveLength(10);
  expect(list.data.next_cursor).toBe('');
  expect(JSON.stringify(list.data)).not.toContain('request_ip');
  const amount = page.getByRole('textbox', { name: '金额（元）' });
  await amount.fill('');
  await expect(page.locator('.r-pay>.r-primary')).toBeDisabled();
  await amount.fill('20.26');
  await amount.press('End');
  await amount.press('x');
  await expect(amount).toHaveValue('20.26');
  expect(await amount.evaluate((e: HTMLInputElement) => e.selectionStart)).toBe(5);
  await page.getByRole('button', { name: '切换外观' }).click();
  await page.getByRole('menuitem', { name: '深色', exact: true }).click();
  await expect(page.locator('.d-app')).toHaveCSS('color-scheme', 'dark');
  await page.getByRole('button', { name: '切换语言' }).click();
  await page.getByRole('menuitem', { name: 'English', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Donations', exact: true })).toBeVisible();
  for (const width of [320, 390, 768, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.screenshot({ path: 'test-results/home-dark-en.png', fullPage: true });
});
test('OIDC signs in, shared account and administration, confirm selected operations', async ({ page }) => {
  await login(page);
  const headerHeight = await page.locator('.d-top').evaluate((e) => e.getBoundingClientRect().height);
  await page.getByRole('button', { name: '账号信息', exact: true }).click();
  await page.getByRole('menuitem', { name: '站点管理', exact: true }).click();
  await expect(page.getByRole('heading', { name: '概览', exact: true })).toBeVisible();
  await expect(page.locator('.overview-grid thead button')).toHaveCount(6);
  expect(await page.locator('.d-top').evaluate((e) => e.getBoundingClientRect().height)).toBe(headerHeight);
  await page.getByRole('button', { name: '订单', exact: true }).click();
  await expect(page.locator('tbody tr').first()).toBeVisible();
  await expect(page.getByRole('button', { name: '导出', exact: true })).toHaveCount(0);
  await page.locator('tbody .custom-check').first().check();
  await page.getByRole('button', { name: '批量操作', exact: true }).click();
  await page.getByRole('menuitem', { name: '隐藏留言', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate((e) => e.scrollWidth <= e.clientWidth)).toBe(true);
  await dialog.getByRole('button', { name: '确认', exact: true }).click();
  await expect(page.getByRole('heading', { name: '操作结果' })).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).last().click();
  await page
    .getByRole('button', { name: /订单详情/ })
    .first()
    .click();
  await expect(page.getByRole('button', { name: '手动补单', exact: true })).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).first().click();
  await page
    .getByRole('navigation', { name: '站点管理', exact: true })
    .getByRole('button', { name: '用户', exact: true })
    .click();
  await page
    .getByRole('button', { name: /用户详情/ })
    .first()
    .click();
  await expect(page.getByRole('heading', { name: '用户详情' })).toBeVisible();
  await page.screenshot({ path: 'test-results/admin-user.png', fullPage: true });
});
test('real payment create, RSA notification and own history through the embedded binary', async ({
  page,
}) => {
  await login(page);
  await page.getByRole('textbox', { name: '金额（元）' }).fill('0.01');
  await page.getByRole('textbox', { name: '留言（选填）' }).fill('浏览器支付验证\n第二行');
  await page.getByRole('button', { name: '捐赠 · 支付宝', exact: true }).click();
  await expect(page.locator('.r-index-row').filter({ hasText: '¥0.01' })).toBeVisible();
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect(page.locator('.r-featured')).toContainText('浏览器支付验证');
  await page.getByRole('button', { name: '订单详情', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('已支付');
});
