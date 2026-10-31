import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';
import type { TaskItem } from '../src/lib/types';

async function loginTasks(page: Page) {
  await page.goto('/');
  await page.getByRole('button', { name: '知道了', exact: true }).click();
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('checkbox', { name: '同意用户协议与隐私政策' }).check();
  await page.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect(page.getByRole('button', { name: '账号信息', exact: true })).toBeVisible();
  await page.goto('/admin');
  await page.locator('.a-side').getByRole('button', { name: '任务', exact: true }).click();
  await expect(page.locator('.task-table tbody tr').first()).toBeVisible();
  await expect(page.getByRole('checkbox', { name: '自动刷新', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '刷新任务', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '重试所选', exact: true })).toBeDisabled();
}
async function select(page: Page, label: string, option: string) {
  if (!(await page.getByRole('combobox', { name: label, exact: true }).isVisible())) {
    const more = page.getByRole('button', { name: '更多筛选', exact: true });
    if ((await more.getAttribute('aria-expanded')) !== 'true') await more.click();
  }
  await page.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}
async function readTasks(page: Page, filter = ''): Promise<TaskItem[]> {
  const response = await page.request.get(`/api/v1/admin/task/list?page_size=100&${filter}`);
  expect(response.ok()).toBe(true);
  const envelope = await response.json();
  expect(envelope.code).toBe('OK');
  return envelope.data.items;
}

test('real task API reads and persistent history link to existing order detail', async ({ page }) => {
  await loginTasks(page);
  const tasks = await readTasks(page, 'status=failed&kind=payment&sort=total_attempts&direction=desc');
  expect(tasks.length).toBeGreaterThan(1);
  const task = tasks[0];
  await page.getByRole('textbox', { name: '搜索任务' }).fill(task.id);
  await expect(page.locator('.task-table tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: `任务详情 ${task.merchant_order_no}`, exact: true }).click();
  const detail = page.getByRole('dialog', { name: '任务详情', exact: true });
  await expect(detail).toContainText('fixture-task-0-0');
  await expect(detail.locator('.task-timeline li')).toHaveCount(10);
  await expect(detail).toContainText('历史从升级后开始记录');
  const title = detail.locator('.task-detail-title');
  await expect(title.locator('h3')).toHaveText('支付核验');
  await expect(title.locator('.a-badge')).toHaveText('失败');
  const titleBounds = await title.locator('h3, .a-badge').evaluateAll((elements) =>
    elements.map((element) => {
      const box = element.getBoundingClientRect();
      return { x: box.x, y: box.y, height: box.height };
    }),
  );
  expect(titleBounds[0].x).toBeLessThan(titleBounds[1].x);
  expect(
    Math.abs(titleBounds[0].y + titleBounds[0].height / 2 - titleBounds[1].y - titleBounds[1].height / 2),
  ).toBeLessThan(2);
  await expect(
    detail.locator('.task-detail-footer').getByRole('button', { name: '重试', exact: true }),
  ).toBeVisible();
  await expect(
    detail.locator('.task-detail-footer').getByRole('button', { name: '下一页', exact: true }),
  ).toBeVisible();
  await detail.getByRole('button', { name: '下一页', exact: true }).click();
  await expect(detail.locator('.a-pagination')).toContainText('2 /');
  const history = await (
    await page.request.get(`/api/v1/admin/task/history?id=${task.id}&page=2&page_size=10`)
  ).json();
  expect(history.data.items).toHaveLength(10);
  await detail.locator('.task-target').click();
  const order = page.getByRole('dialog', { name: '订单详情', exact: true });
  await expect(order).toContainText(task.merchant_order_no);
  await order.getByRole('button', { name: '关闭', exact: true }).first().click();
  await expect(order).toHaveCount(0);
  await expect(detail).toHaveCSS('opacity', '1');
  await expect(detail).toBeVisible();
  await page.screenshot({
    path: '../.superpowers/sdd/2026-10-31-task-queue/screenshots/task-queue-history.png',
    fullPage: false,
  });
});

test('polls task data without remounting and revokes dialogs when access is lost', async ({ page }) => {
  await page.clock.install();
  await loginTasks(page);
  const task = (await readTasks(page))[0];
  await page.getByRole('textbox', { name: '搜索任务' }).fill(task.id);
  await expect(page.locator('.task-table tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: `任务详情 ${task.merchant_order_no}`, exact: true }).click();
  const detail = page.getByRole('dialog', { name: '任务详情', exact: true });
  await expect(detail.locator('.task-detail-grid')).toBeVisible();
  await detail.getByRole('button', { name: '关闭', exact: true }).click();
  await expect(detail).toHaveCount(0);
  await page.getByRole('button', { name: '查看趋势数据', exact: true }).click();
  const curve = page.locator('.task-trend polyline').first();
  await curve.evaluate((element) => element.setAttribute('data-poll-identity', 'original'));
  const polled = page.waitForResponse((res) => res.url().includes('/admin/task/list?'));
  await page.clock.runFor(5000);
  await polled;
  await expect(curve).toHaveAttribute('data-poll-identity', 'original');
  await expect(page.getByRole('button', { name: '查看趋势数据', exact: true })).toHaveAttribute(
    'aria-expanded',
    'true',
  );
  await expect(page.getByRole('textbox', { name: '搜索任务' })).toHaveValue(task.id);
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  let polls = 0;
  page.on('request', (request) => {
    if (request.url().includes('/admin/task/')) polls++;
  });
  await page.clock.runFor(15000);
  expect(polls).toBe(0);
  await page.evaluate(() => {
    Reflect.deleteProperty(document, 'hidden');
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.clock.resume();
  const profile = await (await page.request.get('/api/v1/user/profile')).json();
  await page.route('**/api/v1/user/profile', (route) =>
    route.fulfill({ json: { ...profile, data: { ...profile.data, admin: false } } }),
  );
  await page.route('**/api/v1/admin/task/detail?**', (route) =>
    route.fulfill({ status: 403, json: { code: 'FORBIDDEN', data: null } }),
  );
  await page.getByRole('button', { name: `任务详情 ${task.merchant_order_no}`, exact: true }).click();
  await expect(page.getByText('此账号没有站点管理权限。')).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('single and bulk retries use real API and show each result', async ({ page }) => {
  await loginTasks(page);
  await select(page, '状态', '失败');
  const before = await readTasks(page, 'status=failed');
  const task = before.find((item) => item.retryable);
  if (!task) throw new Error('The real fixture must expose a retryable failed task');
  await page.getByRole('button', { name: `重试 ${task.merchant_order_no}`, exact: true }).click();
  const confirm = page.getByRole('dialog', { name: '重试任务', exact: true });
  await expect(confirm).toBeVisible();
  expect(await confirm.evaluate((el) => getComputedStyle(el).boxShadow)).not.toBe('none');
  await confirm.getByRole('button', { name: '确认', exact: true }).click();
  const results = page.getByRole('dialog', { name: '操作结果', exact: true });
  await expect(results).toContainText('已加入队列');
  await results.getByRole('button', { name: '关闭', exact: true }).last().click();
  const history = await (await page.request.get(`/api/v1/admin/task/history?id=${task.id}`)).json();
  expect(history.data.items.some((event: { event: string }) => event.event === 'retried')).toBe(true);
  const remaining = await readTasks(page, 'status=failed');
  const retryable = remaining.find((item) => item.retryable);
  const inapplicable = remaining.find((item) => !item.retryable);
  if (!retryable || !inapplicable) throw new Error('The real fixture must expose mixed retry eligibility');
  await page.getByRole('checkbox', { name: `选择任务 ${retryable.merchant_order_no}`, exact: true }).check();
  await page
    .getByRole('checkbox', { name: `选择任务 ${inapplicable.merchant_order_no}`, exact: true })
    .check();
  await page.getByRole('button', { name: '重试所选', exact: true }).click();
  await page
    .getByRole('dialog', { name: '重试所选', exact: true })
    .getByRole('button', { name: '确认', exact: true })
    .click();
  await expect(results.locator('.operation-items>div')).toHaveCount(2);
  await expect(results).toContainText('已加入队列');
  await expect(results).toContainText('不适用');
  await results.getByRole('button', { name: '关闭', exact: true }).last().click();
  await page.locator('.a-side').getByRole('button', { name: '审计', exact: true }).click();
  await select(page, '操作', '任务操作');
  await expect(page.locator('tbody')).toContainText('重试任务');
  await expect(page.locator('tbody')).not.toContainText('not_applicable');
});

test('filters, sorting and trend range preserve current queue meaning and recover errors', async ({
  page,
}) => {
  await loginTasks(page);
  const counts = await page.locator('.task-stat strong').allTextContents();
  for (const source of [
    'automatic',
    'manual',
    'browser_return',
    'admin',
    'admin_refresh',
    'admin_retry',
    'close_followup',
  ]) {
    await readTasks(page, `source=${source}`);
  }
  await select(page, '时间范围', '30d');
  await expect(page.locator('.task-stat strong')).toHaveText(counts);
  const sorted = page.waitForResponse(
    (res) => res.url().includes('/admin/task/list?') && res.url().includes('sort=total_attempts'),
  );
  await page.getByRole('button', { name: '已记录执行', exact: true }).click();
  await sorted;
  await select(page, '类型', '属地查询');
  await select(page, '来源', '管理员刷新');
  const expected = await readTasks(page, 'kind=geo&source=admin_refresh');
  await expect(page.locator('.task-table tbody tr')).toHaveCount(expected.length);
  if (expected.length) await expect(page.locator('.task-table tbody')).toContainText('管理员刷新');
  let fail = true;
  await page.route('**/api/v1/admin/task/list?**', async (route) => {
    if (fail) {
      fail = false;
      await route.fulfill({ status: 503, body: 'synthetic outage' });
    } else await route.continue();
  });
  await page.getByRole('textbox', { name: '搜索任务' }).fill('fixture');
  await expect(page.locator('.request-error')).toBeVisible();
  await page.locator('.request-error').getByRole('button', { name: '重试', exact: true }).click();
  await expect(page.locator('.request-error')).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: '类型', exact: true })).toContainText('属地查询');
});

test('task workspace stays contained and five tabs keep equal height across sizes and themes', async ({
  page,
}) => {
  await loginTasks(page);
  for (const theme of ['light', 'dark']) {
    await page.evaluate((value) => {
      document.querySelector<HTMLElement>('.d-app')?.style.setProperty('color-scheme', value);
    }, theme);
    for (const width of [320, 390, 768, 1280]) {
      await page.setViewportSize({ width, height: 950 });
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        `${theme} ${width}`,
      ).toBe(true);
      const heights = await page
        .locator('.a-side .a-nav-item')
        .evaluateAll((items) => items.map((el) => Math.round(el.getBoundingClientRect().height)));
      expect(heights).toHaveLength(5);
      expect(new Set(heights).size).toBe(1);
      const tools = page.locator('.task-tools');
      const controls = await tools.locator(':scope > *').evaluateAll((elements) =>
        elements.map((element) => {
          const box = element.getBoundingClientRect();
          return { x: box.x, right: box.right, y: box.y, height: box.height };
        }),
      );
      const center = controls[0].y + controls[0].height / 2;
      expect(
        controls.every((control) => Math.abs(control.y + control.height / 2 - center) < 2),
        `single toolbar row at ${width}`,
      ).toBe(true);
      expect(controls.every((control) => control.x >= 0 && control.right <= width)).toBe(true);
      if (width <= 390) {
        expect(controls).toHaveLength(3);
        await expect(tools.getByRole('combobox')).toHaveCount(0);
      }
      const chart = await page.locator('.task-trend svg').evaluate((node) => {
        const svg = node as SVGSVGElement;
        const bounds = svg.getBoundingClientRect();
        return { height: bounds.height, fontSize: (12 * bounds.width) / svg.viewBox.baseVal.width };
      });
      expect(chart.fontSize, `axis text must stay readable at ${width}`).toBeGreaterThanOrEqual(9);
      expect(chart.height, `chart must keep a useful height at ${width}`).toBeGreaterThanOrEqual(160);
      await page.locator('.task-table tbody .custom-check').first().check();
      await expect(page.locator('.task-table tbody .task-check > svg').first()).toHaveCSS('opacity', '1');
      await page.getByRole('button', { name: '重试所选', exact: true }).click();
      const confirm = page.getByRole('dialog', { name: '重试所选', exact: true });
      await expect(confirm).toBeVisible();
      const box = await confirm.boundingBox();
      expect(box).not.toBeNull();
      if (box) {
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width);
      }
      await confirm.getByRole('button', { name: '取消', exact: true }).click();
      await expect(confirm).toHaveCount(0);
      await page.screenshot({
        path: `../.superpowers/sdd/2026-10-31-task-queue/screenshots/task-queue-${width}-${theme}.png`,
        fullPage: false,
      });
      if (width === 320 && theme === 'dark') {
        await page.locator('.task-list-panel').scrollIntoViewIfNeeded();
        await page.screenshot({
          path: '../.superpowers/sdd/2026-10-31-task-queue/screenshots/task-queue-320-dark-list.png',
        });
        await page
          .locator('.task-table tbody tr')
          .first()
          .getByRole('button', { name: /^任务详情 / })
          .click();
        const detail = page.getByRole('dialog', { name: '任务详情', exact: true });
        await expect(detail.locator('.task-detail-grid')).toBeVisible();
        await expect(detail).toHaveCSS('opacity', '1');
        await detail.evaluate((node) => {
          node.scrollTop = 0;
        });
        await page.screenshot({
          path: '../.superpowers/sdd/2026-10-31-task-queue/screenshots/task-queue-320-dark-detail.png',
        });
        await detail.getByRole('button', { name: '关闭', exact: true }).click();
        await expect(detail).toHaveCount(0);
      }
      await page.locator('.task-table tbody .custom-check').first().uncheck();
      await expect(page.getByRole('button', { name: '重试所选', exact: true })).toBeDisabled();
    }
  }
});

test('trend and filter disclosure respect reduced motion and keyboard access', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 390, height: 950 });
  await loginTasks(page);
  await expect(page.locator('.task-trend-recording')).toContainText('开始记录于');
  await expect(page.locator('.task-trend polyline').first()).toHaveCSS('animation-name', 'none');
  const data = page.getByRole('button', { name: '查看趋势数据', exact: true });
  await data.focus();
  await page.keyboard.press('Enter');
  const table = page.getByRole('region', { name: '查看趋势数据', exact: true });
  await expect(table).toBeVisible();
  await expect(table.locator('table')).toHaveCSS('font-size', '13px');
  await page.keyboard.press('Enter');
  await expect(table).toHaveAttribute('inert', '');
  const more = page.getByRole('button', { name: '更多筛选', exact: true });
  await more.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('combobox', { name: '类型', exact: true })).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('combobox', { name: '类型', exact: true })).toHaveCount(0);
  await page.keyboard.press('Tab');
  await expect(page.getByRole('combobox', { name: '类型', exact: true })).not.toBeFocused();
});
