import { expect, test } from '@playwright/test';

test('message height changes only when its content is edited', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/');
  const message = page.getByRole('textbox', { name: '留言（选填）' });
  await expect(page.locator('.r-index-row')).toHaveCount(10);
  await message.fill(Array.from({ length: 12 }, () => '一段留言').join(String.fromCharCode(10)));
  await expect.poll(() => message.evaluate((el) => el.clientHeight)).toBeGreaterThan(100);
  await expect
    .poll(() =>
      message.evaluate((el) =>
        Math.abs(el.getBoundingClientRect().height - Number.parseFloat(el.style.height)),
      ),
    )
    .toBeLessThan(0.5);
  const height = await message.evaluate((el) => el.getBoundingClientRect().height);
  for (const width of [1100, 800, 390, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(message).toBeVisible();
    await expect
      .poll(() => message.evaluate((el) => el.getBoundingClientRect().height))
      .toBeCloseTo(height, 0);
  }
  await page.getByRole('button', { name: '下一页', exact: true }).click();
  await page.getByRole('button', { name: '切换外观', exact: true }).click();
  await page.getByRole('menuitem', { name: '深色', exact: true }).click();
  await expect.poll(() => message.evaluate((el) => el.getBoundingClientRect().height)).toBeCloseTo(height, 0);
  await message.fill(Array.from({ length: 220 }, () => '行').join(String.fromCharCode(10)));
  await expect.poll(() => message.evaluate((el) => el.scrollHeight > el.clientHeight + 1)).toBe(true);
  const cardBottom = await page
    .locator('.r-write .r-ticket')
    .evaluate((el) => el.getBoundingClientRect().bottom);
  const bookBottom = await page.locator('.r-book').evaluate((el) => el.getBoundingClientRect().bottom);
  expect(cardBottom).toBeLessThanOrEqual(bookBottom + 2);
  await message.fill('');
  await expect.poll(() => message.evaluate((el) => el.clientHeight)).toBeLessThan(100);
});

test('all administration tabs keep readable non-overlapping cells across viewport widths', async ({
  page,
}) => {
  await page.goto('/');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
  await page.goto('/admin');
  for (const width of [320, 360, 390, 480, 640, 680, 700, 768, 860, 900, 1024, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    for (const tab of ['概览', '订单', '用户', '审计']) {
      await page
        .getByRole('navigation', { name: '站点管理', exact: true })
        .getByRole('button', { name: tab, exact: true })
        .click();
      await expect(page.getByRole('heading', { name: tab, exact: true })).toBeVisible();
      await expect(page.locator('tbody tr').first()).toBeVisible();
      if (tab === '概览') {
        expect(
          await page
            .locator('.d-footer')
            .evaluate((el) => el.getBoundingClientRect().bottom >= innerHeight - 1),
        ).toBe(true);
      }
      for (const select of await page.locator('.a-tools .a-select').all()) {
        expect(
          await select.evaluate((el) => el.getBoundingClientRect().height),
          'single-line filter',
        ).toBeLessThan(56);
      }
      const errors = await page.evaluate(() => {
        const issues: string[] = [];
        if (document.documentElement.scrollWidth > innerWidth + 1) issues.push('document overflow');
        for (const row of document.querySelectorAll('tbody tr')) {
          const cells = [...row.querySelectorAll('td')]
            .map((el) => el.getBoundingClientRect())
            .filter((rect) => rect.width && rect.height);
          for (let a = 0; a < cells.length; a++)
            for (let b = a + 1; b < cells.length; b++) {
              const x = cells[a],
                y = cells[b];
              if (
                Math.min(x.right, y.right) - Math.max(x.left, y.left) > 1 &&
                Math.min(x.bottom, y.bottom) - Math.max(x.top, y.top) > 1
              )
                issues.push(`overlapping cells ${a}/${b}`);
            }
        }
        return issues;
      });
      expect(errors, `${tab} at ${width}px`).toEqual([]);
    }
  }
});
