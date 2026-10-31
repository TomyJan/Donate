import { expect, test } from '@playwright/test';

test('blocked browser storage leaves documents, preferences and sign-in agreement usable', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.addInitScript(() => {
    for (const method of ['getItem', 'setItem', 'removeItem'])
      Object.defineProperty(Storage.prototype, method, {
        value: () => {
          throw new DOMException('Blocked', 'SecurityError');
        },
      });
  });
  await page.goto('/privacy');
  await expect(page.getByRole('heading', { name: '隐私政策', exact: true, level: 1 })).toBeVisible();
  await page.getByRole('button', { name: '切换语言' }).click();
  await page.getByRole('menuitem', { name: 'English', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Privacy Policy', exact: true, level: 1 })).toBeVisible();
  await page.getByRole('button', { name: 'Appearance' }).click();
  await page.getByRole('menuitem', { name: 'Dark', exact: true }).click();
  await expect(page.locator('.d-app')).toHaveCSS('color-scheme', 'dark');
  await page.getByRole('region', { name: 'Cookies and local storage' }).getByRole('button').click();
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Sign in', exact: true });
  await dialog.getByRole('checkbox').check();
  await expect(dialog.getByRole('button', { name: 'OIDC', exact: true })).toBeEnabled();
  await page.goto('/');
  await page.locator('#donation-amount').fill('8.88');
  await expect(page.locator('#donation-amount')).toHaveValue('8.88');
  expect(errors).toEqual([]);
});

test('sign-in requires an explicit, revocable local agreement independent of the cookie notice', async ({
  page,
}) => {
  await page.goto('/');
  const notice = page.getByRole('region', { name: 'Cookie 与本地存储' });
  await expect(notice).toBeVisible();
  await notice.getByRole('button', { name: '知道了', exact: true }).click();
  await page.getByRole('button', { name: '登录', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '登录', exact: true });
  const agreement = dialog.getByRole('checkbox', { name: '同意用户协议与隐私政策' });
  await expect(agreement).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'OIDC', exact: true })).toBeDisabled();
  for (const name of ['用户协议', '隐私政策']) {
    await expect(dialog.getByRole('link', { name, exact: true })).toHaveAttribute('target', '_blank');
  }
  await agreement.check();
  await expect(dialog.getByRole('button', { name: 'OIDC', exact: true })).toBeEnabled();
  await page.reload();
  await expect(notice).toHaveCount(0);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(agreement).toBeChecked();
  await agreement.uncheck();
  await page.reload();
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(agreement).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'OIDC', exact: true })).toBeDisabled();
});

test('legal documents load directly, retain shared navigation and expose both policies in the footer', async ({
  page,
}) => {
  for (const [path, name] of [
    ['/terms', '用户协议'],
    ['/privacy', '隐私政策'],
  ]) {
    await page.goto(path);
    await expect(page.getByRole('heading', { name, exact: true, level: 1 })).toBeVisible();
    const footer = page.locator('footer');
    await expect(footer.getByRole('link', { name: '用户协议', exact: true })).toHaveAttribute(
      'href',
      '/terms',
    );
    await expect(footer.getByRole('link', { name: '隐私政策', exact: true })).toHaveAttribute(
      'href',
      '/privacy',
    );
    await expect(page.getByRole('link', { name: '捐赠 TomyJan', exact: true })).toHaveAttribute('href', '/');
    await page.reload();
    await expect(page.getByRole('heading', { name, exact: true, level: 1 })).toBeVisible();
  }
  await page.goto('/terms');
  await expect(page.locator('main')).toContainText('原则上不提供退款');
  await expect(page.locator('main')).toContainText('不因捐赠而承诺');
  await page.goto('/privacy');
  await expect(page.locator('main')).toContainText('省级');
  await expect(page.locator('main')).toContainText('IP');
});

test('cookie notice is bottom-floating, responsive and remembers dismissal without a footer entry', async ({
  page,
}) => {
  await page.goto('/');
  const notice = page.getByRole('region', { name: 'Cookie 与本地存储' });
  await expect(page.locator('footer').getByRole('button', { name: 'Cookie', exact: true })).toHaveCount(0);
  for (const width of [1280, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(notice).toBeVisible();
    await expect(notice).toHaveCSS('position', 'fixed');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const box = await notice.boundingBox();
    expect(box?.y).toBeGreaterThan(500);
    for (const name of ['用户协议', '隐私政策'])
      await expect(notice.getByRole('link', { name, exact: true })).toBeVisible();
  }
  await page.getByRole('button', { name: '切换外观' }).click();
  await page.getByRole('menuitem', { name: '深色', exact: true }).click();
  await notice.getByRole('link', { name: '隐私政策', exact: true }).click();
  await expect(page.getByRole('heading', { name: '隐私政策', exact: true, level: 1 })).toBeVisible();
  await expect(notice).toBeVisible();
  await notice.getByRole('button', { name: '知道了', exact: true }).click();
  await expect(notice).toHaveCount(0);
  await page.reload();
  await expect(notice).toHaveCount(0);
  await expect(page.locator('footer').getByRole('button', { name: 'Cookie', exact: true })).toHaveCount(0);
});
