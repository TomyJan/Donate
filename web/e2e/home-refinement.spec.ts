import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';
import type { RecordItem } from '../src/lib/types';

const records: RecordItem[] = Array.from({ length: 6 }, (_, index) => ({
  is_mine: index === 0,
  id: `home-receipt-${index}`,
  nickname: `纸片 ${index + 1}`,
  avatar_url: '',
  amount_fen: index === 2 ? 10000000 : 2026,
  date: `2026-10-${String(29 - index).padStart(2, '0')}T08:35:00Z`,
  province: '上海',
  message: index === 1 ? '一张更长的收据\n第二行留言\n第三行留言\n第四行留言' : '谢谢支持',
  status: index === 1 ? 'pending' : 'paid',
}));

async function home(page: Page, holdMore = false) {
  let releaseMore = () => {};
  const moreReady = new Promise<void>((resolve) => {
    releaseMore = resolve;
  });
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    let data: unknown = {};
    if (url.pathname.endsWith('/site/info'))
      data = {
        csrf_token: 'home-test-csrf',
        logo: '',
        name: 'Donate',
        payment_enabled: true,
        providers: ['oidc'],
        version: { built_at: '', commit: '', tag: '', version: 'test' },
      };
    if (url.pathname.endsWith('/user/profile'))
      data = {
        id: 'home-test-user',
        admin: false,
        nickname: '纸片',
        avatar_url: '',
        bindings: [],
        csrf_token: 'home-test-csrf',
        expires_at: '2099-01-01T00:00:00Z',
        provider: 'oidc',
      };
    if (url.pathname.endsWith('/order/list')) {
      const next = !!url.searchParams.get('cursor');
      if (next) {
        if (holdMore) await moreReady;
        else await new Promise((resolve) => setTimeout(resolve, 250));
      }
      const items = (next ? records.slice(3) : records.slice(0, 3)).map((record) => ({
        ...record,
        is_mine: url.searchParams.get('only_mine') === 'true' || record.is_mine,
      }));
      data = { items, next_cursor: next ? '' : 'page2' };
    }
    await route.fulfill({ json: { code: 'OK', message: '', request_id: 'home-test', data } });
  });
  await page.setViewportSize({ width: 1280, height: 1000 });
  await page.goto('/');
  await expect(page.locator('.r-index-row')).toHaveCount(3);
  await page.evaluate(() => document.fonts.ready);
  await expect
    .poll(() =>
      page.locator('.r-stack').evaluate((el) => {
        const receipt = el.querySelector<HTMLElement>('.r-featured');
        return receipt ? Math.abs(el.getBoundingClientRect().height - receipt.offsetHeight) : Infinity;
      }),
    )
    .toBeLessThan(0.5);
  return releaseMore;
}

test('amount presets share the moving selection and keep manual amount input authoritative', async ({
  page,
}) => {
  await home(page);
  const presets = page.locator('.r-presets');
  await expect(presets.locator('.d-selection-highlight')).toHaveCount(1);
  const press = presets.getByRole('button', { name: '648', exact: true });
  const box = await press.boundingBox();
  if (!box) throw new Error('Missing amount preset');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  try {
    await expect(press).toHaveCSS('transform', 'none');
    await expect
      .poll(() =>
        press
          .locator('.d-selection-content')
          .evaluate((content) => Number.parseFloat(getComputedStyle(content).scale)),
      )
      .toBeLessThan(0.99);
  } finally {
    await page.mouse.up();
  }
  await expect(press).toHaveCSS('transform', 'none');
  for (const width of [1280, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    for (const amount of ['648', '0.01', '256', '8.88']) {
      await presets.getByRole('button', { name: amount, exact: true }).click();
      await expect(presets.getByRole('button', { name: amount, exact: true })).toHaveAttribute(
        'aria-pressed',
        'true',
      );
      await expect(page.getByRole('textbox', { name: '金额（元）', exact: true })).toHaveValue(amount);
      await expect(presets.locator('.d-selection-highlight')).toHaveCount(1);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.getByRole('textbox', { name: '金额（元）', exact: true }).fill('19.99');
  await expect(presets.locator('.d-selection-highlight')).toHaveCount(0);
  await page.getByRole('textbox', { name: '金额（元）', exact: true }).fill('8.88');
  await expect(presets.getByRole('button', { name: '8.88', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await presets.getByRole('button', { name: '50', exact: true }).click();
  await expect(presets.locator('.d-selection-highlight')).toHaveCSS('transform', 'none');
  await expect(presets.locator('.d-selection-highlight')).toHaveCSS('translate', 'none');
  await expect(presets.locator('.d-selection-content').first()).toHaveCSS('scale', 'none');
});

test('more expands appended records without replaying old rows or resizing the message', async ({ page }) => {
  const releaseMore = await home(page, true);
  const message = page.getByRole('textbox', { name: '留言（选填）' });
  await message.fill('留言高度只随输入变化\n第二行\n第三行\n第四行');
  await expect
    .poll(() =>
      message.evaluate((el) =>
        Math.abs(el.getBoundingClientRect().height - Number.parseFloat(el.style.height)),
      ),
    )
    .toBeLessThan(0.5);
  const messageHeight = await message.evaluate((el) => el.getBoundingClientRect().height);
  const more = page.locator('.r-more button');
  await more.click();
  await expect(more).toBeDisabled();
  releaseMore();
  await expect(page.locator('.r-index-row')).toHaveCount(6);
  const samples = await page.evaluate(async () => {
    const frames = [];
    for (let i = 0; i < 32; i++) {
      const list = document.querySelector('.r-index');
      if (!list) throw new Error('Record index disappeared while appending');
      const rows = list.querySelectorAll('.r-index-row');
      frames.push({
        height: list.getBoundingClientRect().height,
        oldOpacity: getComputedStyle(rows[0]).opacity,
        newOpacity: getComputedStyle(rows[3]).opacity,
      });
      await new Promise(requestAnimationFrame);
    }
    return frames;
  });
  expect(samples.at(-1)?.height - samples[0].height).toBeGreaterThan(40);
  expect(samples.some((frame) => Number(frame.newOpacity) < 0.95)).toBe(true);
  expect(samples.every((frame) => frame.oldOpacity === '1')).toBe(true);
  expect(await message.evaluate((el) => el.getBoundingClientRect().height)).toBe(messageHeight);
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect(page.locator('.r-index-row')).toHaveCount(3);
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect(page.locator('.r-index-row')).toHaveCount(6);
  expect(
    await page
      .locator('.r-index-row')
      .evaluateAll((rows) => rows.every((el) => getComputedStyle(el).opacity === '1')),
  ).toBe(true);
  expect(await message.evaluate((el) => el.getBoundingClientRect().height)).toBe(messageHeight);
});

test('whole receipts flip in three dimensions and follow the navigation direction', async ({ page }) => {
  await home(page);
  const forward = await page.evaluate(async () => {
    (document.querySelectorAll('.r-page-nav button')[1] as HTMLButtonElement).click();
    const frames = [];
    for (let i = 0; i < 36; i++) {
      await new Promise(requestAnimationFrame);
      const receipt = document.querySelector('.r-featured:last-child');
      if (!receipt) throw new Error('Current receipt disappeared during forward flip');
      const matrix = new DOMMatrixReadOnly(getComputedStyle(receipt).transform);
      frames.push({
        x: matrix.m13,
        y: matrix.m23,
        height: document.querySelector('.r-stack')?.getBoundingClientRect().height,
      });
    }
    return frames;
  });
  expect(forward.some((frame) => Math.abs(frame.x) + Math.abs(frame.y) > 0.1)).toBe(true);
  expect(forward.at(-1)?.height - forward[0].height).toBeGreaterThan(40);
  expect(
    forward.every((frame, index) => !index || Math.abs(frame.height - forward[index - 1].height) < 35),
  ).toBe(true);
  const backward = await page.evaluate(async () => {
    (document.querySelectorAll('.r-page-nav button')[0] as HTMLButtonElement).click();
    const frames = [];
    for (let i = 0; i < 36; i++) {
      await new Promise(requestAnimationFrame);
      const receipt = document.querySelector('.r-featured:last-child');
      if (!receipt) throw new Error('Current receipt disappeared during backward flip');
      const matrix = new DOMMatrixReadOnly(getComputedStyle(receipt).transform);
      frames.push(matrix.m13);
    }
    return frames;
  });
  expect(forward.find((frame) => Math.abs(frame.x) > 0.03)?.x).toBeGreaterThan(0);
  expect(backward.find((value) => Math.abs(value) > 0.03)).toBeLessThan(0);
});

test('rapid selection never makes departing receipts interactive or restores old content', async ({
  page,
}) => {
  await home(page);
  const samples = await page.evaluate(async () => {
    const rows = document.querySelectorAll<HTMLButtonElement>('.r-index-row');
    const frames = [];
    for (const selected of [1, 2, 1, 2]) {
      rows[selected].click();
      await new Promise(requestAnimationFrame);
    }
    for (let i = 0; i < 40; i++) {
      await new Promise(requestAnimationFrame);
      frames.push(
        [...document.querySelectorAll('.r-featured:not([aria-hidden="true"])')].map(
          (el) => el.querySelector('strong')?.textContent,
        ),
      );
    }
    return frames;
  });
  expect(samples.every((frame) => frame.length === 1 && frame[0] === '纸片 3')).toBe(true);
  await expect(page.locator('.r-featured')).toHaveCount(1);
  await expect(page.locator('.r-featured')).toContainText('纸片 3');
});

test('mine keeps the same amount size and alignment and reduced motion suppresses flips', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await home(page);
  const amount = page.locator('.r-receipt-amount');
  const normal = await amount.evaluate((el) => ({
    font: getComputedStyle(el).fontSize,
    left: el.getBoundingClientRect().left,
    top: el.getBoundingClientRect().top,
  }));
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect(page.locator('.r-receipt-action')).toBeVisible();
  await expect(amount).toHaveCSS('font-size', normal.font);
  const mine = await amount.boundingBox();
  expect(mine?.x).toBeCloseTo(normal.left, 0);
  expect(mine?.y).toBeCloseTo(normal.top, 0);
  await expect(page.locator('.r-featured .r-status')).toHaveCount(0);
  await page.locator('.r-page-nav button').nth(1).click();
  await expect(page.locator('.r-featured')).toHaveCount(1);
  const matrix = await page.locator('.r-featured').evaluate((el) => {
    const transform = new DOMMatrixReadOnly(getComputedStyle(el).transform);
    return [transform.m13, transform.m23];
  });
  expect(matrix).toEqual([0, 0]);
  await expect(page.locator('.r-featured .r-status')).toContainText('待支付');
  await expect(page.locator('.r-receipt-action button')).toHaveCount(2);
});

test('amount and right-aligned actions share one row on narrow receipts including maximum amounts', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await home(page);
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  for (const language of ['zh-CN', 'en']) {
    if (language === 'en') {
      await page.getByRole('button', { name: '切换语言', exact: true }).click();
      await page.getByRole('menuitem', { name: 'English', exact: true }).click();
    }
    for (const width of [320, 390, 1280]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const index of [0, 1, 2]) {
        await page.locator('.r-index-row').nth(index).click();
        await expect(page.locator('.r-featured')).toHaveCount(1);
        const layout = await page.locator('.r-receipt-value').evaluate((el) => {
          const amount = el.querySelector<HTMLElement>('.r-receipt-amount');
          const action = el.querySelector<HTMLElement>('.r-receipt-action');
          if (!amount || !action) throw new Error('Receipt amount or actions missing');
          const amountBox = amount.getBoundingClientRect();
          const actionBox = action.getBoundingClientRect();
          const rowBox = el.getBoundingClientRect();
          const controls = [...action.querySelectorAll('button')].map((button) => button.offsetTop);
          return {
            sameRow: Math.min(amountBox.bottom, actionBox.bottom) > Math.max(amountBox.top, actionBox.top),
            separated: amount.scrollWidth <= amount.clientWidth && amountBox.right < actionBox.left + 1,
            contained: actionBox.right <= rowBox.right + 1,
            controlRows: new Set(controls).size,
            overflow: document.documentElement.scrollWidth > innerWidth,
            fit: {
              amount: amount.clientWidth,
              scroll: amount.scrollWidth,
              font: getComputedStyle(amount).fontSize,
              amountRight: amountBox.right,
              actionLeft: actionBox.left,
            },
          };
        });
        expect(layout, `receipt ${index} at ${width}px: ${JSON.stringify(layout)}`).toMatchObject({
          sameRow: true,
          separated: true,
          contained: true,
          controlRows: 1,
          overflow: false,
        });
      }
    }
  }
});

test('public view offers actions on owned receipts and preserves the province', async ({ page }) => {
  await home(page);
  const receipt = page.locator('.r-featured:not(.r-leaving)');
  await expect(page.getByRole('button', { name: '只看我的', exact: true })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  await expect(receipt.getByRole('button', { name: '订单详情', exact: true })).toBeVisible();
  await expect(receipt.locator('.r-meta')).toContainText('上海');
  await page.locator('.r-index-row').nth(1).click();
  await expect(receipt).toContainText('纸片 2');
  await expect(receipt.getByRole('button', { name: '订单详情', exact: true })).toHaveCount(0);
});
