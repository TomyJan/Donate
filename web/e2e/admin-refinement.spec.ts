import { expect, test } from '@playwright/test';

async function login(page: import('@playwright/test').Page) {
  await page.goto('/');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
  await page.goto('/admin');
}

test('admin lists show content instead of IDs and offer page size', async ({ page }) => {
  await login(page);
  await page
    .getByRole('navigation', { name: '站点管理', exact: true })
    .getByRole('button', { name: '订单', exact: true })
    .click();
  await expect(page.locator('tbody tr').first()).toBeVisible();
  await expect(page.locator('td[data-field=created_at] button')).toHaveCount(0);
  await expect(page.locator('th[data-field=message]')).toBeVisible();
  const size = page.locator('.a-foot').getByRole('combobox', { name: '每页数量', exact: true });
  await size.click();
  await page.getByRole('option', { name: '10 / 页', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(10);
  await expect(page.locator('.a-pagination')).toBeVisible();
  await page
    .getByRole('navigation', { name: '站点管理', exact: true })
    .getByRole('button', { name: '审计', exact: true })
    .click();
  await expect(page.locator('.a-audit tbody tr').first()).toBeVisible();
  await expect(page.locator('.a-audit td[data-field=details]')).toHaveCount(0);
  await expect(page.locator('.a-audit td[data-field=action]').first()).not.toContainText('order.');
});

test('shared header keeps its height, hides downwards and returns upwards', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 780 });
  await login(page);
  const header = page.locator('.d-top');
  const height = await header.evaluate((el) => el.getBoundingClientRect().height);
  const nav = page.getByRole('navigation', { name: '站点管理', exact: true });
  for (const tab of ['订单', '用户', '审计', '概览']) {
    await nav.getByRole('button', { name: tab, exact: true }).click();
    await expect(page.getByRole('heading', { name: tab, exact: true })).toBeVisible();
    expect(await header.evaluate((el) => el.getBoundingClientRect().height)).toBe(height);
  }
  await nav.getByRole('button', { name: '订单', exact: true }).click();
  await expect(page.locator('tbody tr').first()).toBeVisible();
  await page.mouse.wheel(0, 650);
  await expect(header).toHaveAttribute('data-hidden', 'true');
  await expect(page.getByRole('button', { name: '回到顶部', exact: true })).toHaveCount(0);
  await page.mouse.wheel(0, -120);
  await expect(page.getByRole('button', { name: '回到顶部', exact: true })).toBeVisible();
  await expect(header).toHaveAttribute('data-hidden', 'false');
  await page.getByRole('button', { name: '回到顶部', exact: true }).click();
  await expect.poll(() => page.evaluate(() => scrollY)).toBeLessThan(2);
});

test('overview range, message preview and audit targets use their actual details', async ({ page }) => {
  await login(page);
  const range = page.getByRole('combobox', { name: '时间范围', exact: true });
  await expect(range).toHaveText('30d');
  await range.click();
  const request = page.waitForResponse((response) => response.url().includes('/admin/overview?range=all'));
  await page.getByRole('option', { name: '全部', exact: true }).click();
  expect((await (await request).json()).code).toBe('OK');
  await range.click();
  await page.getByRole('option', { name: '自定义', exact: true }).click();
  const custom = page.getByRole('dialog', { name: '自定义', exact: true });
  await expect(custom).toBeVisible();
  await expect(custom.getByRole('button', { name: '开始日期', exact: true })).toBeVisible();
  const customResponse = page.waitForResponse((response) =>
    response.url().includes('/admin/overview?range=custom'),
  );
  await custom.getByRole('button', { name: '应用', exact: true }).click();
  expect((await (await customResponse).json()).code).toBe('OK');
  await expect(custom).toHaveCount(0);
  await expect(page.getByText('全部现存订单', { exact: true })).toHaveCount(0);
  const nav = page.getByRole('navigation', { name: '站点管理', exact: true });
  await nav.getByRole('button', { name: '订单', exact: true }).click();
  const message = page.getByRole('button', { name: '查看留言', exact: true }).first();
  await expect(message).toBeVisible();
  const text = await message.textContent();
  await message.click();
  await expect(page.locator('.message-pop')).toHaveText(text ?? '');
  await page.keyboard.press('Escape');
  await nav.getByRole('button', { name: '审计', exact: true }).click();
  const target = page.locator('.audit-target:not(:disabled)').first();
  await expect(target).toBeVisible();
  await target.click();
  await expect(page.getByRole('dialog', { name: '订单详情', exact: true })).toBeVisible();
});

test('a running bulk operation cannot be cancelled or dismissed', async ({ page }) => {
  await login(page);
  await page
    .getByRole('navigation', { name: '站点管理', exact: true })
    .getByRole('button', { name: '订单', exact: true })
    .click();
  await page.locator('tbody .custom-check').first().check();
  await page.getByRole('button', { name: '批量操作', exact: true }).click();
  await page.getByRole('menuitem', { name: '隐藏留言', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: '隐藏留言', exact: true });
  let release = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/v1/admin/order/action/execute', async (route) => {
    await gate;
    await route.continue();
  });
  try {
    const executing = page.waitForRequest('**/api/v1/admin/order/action/execute');
    await confirmation.getByRole('button', { name: '确认', exact: true }).click();
    await executing;
    await expect(confirmation.getByRole('button', { name: '取消', exact: true })).toBeDisabled();
    await expect(confirmation.getByRole('button', { name: '关闭', exact: true })).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(confirmation).toBeVisible();
  } finally {
    release();
  }
  await expect(page.getByRole('dialog', { name: '操作结果', exact: true })).toBeVisible();
});
