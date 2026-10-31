import type { Page } from '@playwright/test';
import { expect, test } from '@playwright/test';
import { LEGAL_VERSION } from '../src/lib/legal';

const orderID = '68f15e68-143e-4b30-a2a9-87c0df4c5479';
const userID = '11644e41-c098-4c32-b44e-96926c19df18';
const paidOrder = {
  id: orderID,
  user_id: userID,
  nickname: '付款测试',
  avatar_url: '',
  amount_fen: 888,
  channel: 'alipay',
  status: 'paid',
  message: '这张收据会归入记录。',
  province: '广东',
  created_at: '2026-10-30T08:00:00Z',
  paid_at: '2026-10-30T08:01:00Z',
  expires_at: '2026-10-30T08:30:00Z',
  merchant_order_no: 'return-test',
  trade_no: 'gateway-confirmed',
  api_trade_no: '',
  protocol: 'v2',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
};
async function prepare(page: Page, options: { status?: string; error?: string; signedIn?: boolean } = {}) {
  let paid = options.status !== 'pending';
  let failed = !!options.error;
  await page.addInitScript((version) => {
    localStorage.setItem('donate:cookie-notice', version);
    sessionStorage.setItem(
      'donate-draft',
      JSON.stringify({ amount: '8.88', channel: 'alipay', message: '保留的草稿' }),
    );
  }, LEGAL_VERSION);
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/user/profile') && options.signedIn === false)
      return route.fulfill({ status: 401, json: { code: 'UNAUTHORIZED', data: null } });
    if (path.endsWith('/order/detail')) {
      if (failed) return route.fulfill({ json: { code: options.error, data: null } });
      return route.fulfill({
        json: { code: 'OK', data: { ...paidOrder, status: paid ? 'paid' : 'pending' } },
      });
    }
    let data: unknown = {};
    if (path.endsWith('/site/info'))
      data = {
        providers: ['oidc'],
        payment_enabled: true,
        version: { version: 'test', commit: '', tag: '' },
      };
    if (path.endsWith('/user/profile'))
      data = {
        id: userID,
        nickname: '付款测试',
        avatar_url: '',
        bindings: [],
        admin: false,
        csrf_token: 'test',
        expires_at: '2099-01-01T00:00:00Z',
      };
    if (path.endsWith('/order/list'))
      data = {
        items: paid
          ? [
              {
                id: orderID,
                is_mine: options.signedIn !== false,
                nickname: paidOrder.nickname,
                avatar_url: '',
                amount_fen: paidOrder.amount_fen,
                message: paidOrder.message,
                province: paidOrder.province,
                status: 'paid',
                date: paidOrder.paid_at,
              },
            ]
          : [],
        next_cursor: '',
      };
    await route.fulfill({ json: { code: 'OK', data } });
  });
  return {
    paid: () => {
      paid = true;
    },
    recover: () => {
      failed = false;
    },
  };
}

test('payment labels center on the entire split button at every width and return state', async ({ page }) => {
  await prepare(page, { error: 'UNAVAILABLE' });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const callback of [false, true]) {
    await page.goto(callback ? `/?payment=returned&order=${orderID}` : '/');
    const ticket = page.locator('.r-write .r-ticket');
    if (callback) await expect(ticket).toHaveAttribute('data-payment-phase', 'error');
    for (const width of [320, 390, 768, 1280]) {
      await page.setViewportSize({ width, height: 900 });
      await expect
        .poll(async () =>
          page.locator('.r-pay').evaluate((element) => {
            const label = element.querySelector('.payment-button-label');
            if (!label) return Number.POSITIVE_INFINITY;
            const range = document.createRange();
            range.selectNodeContents(label);
            const text = range.getBoundingClientRect();
            const button = element.getBoundingClientRect();
            return Math.abs((text.left + text.right) / 2 - (button.left + button.right) / 2);
          }),
        )
        .toBeLessThanOrEqual(1);
      const bounds = await page.locator('.r-pay').evaluate((element) => {
        const label = element.querySelector('.payment-button-label')?.getBoundingClientRect();
        const menu = element.querySelector('.r-channel')?.getBoundingClientRect();
        if (!label || !menu) throw new Error('Payment controls are missing');
        return { labelRight: label.right, menuLeft: menu.left };
      });
      expect(bounds.labelRight).toBeLessThanOrEqual(bounds.menuLeft);
      if (callback) await expect(ticket.locator('.r-channel')).toBeDisabled();
    }
  }
});

test('the composer stays expanded on narrow screens without a collapse control', async ({ page }) => {
  await prepare(page, { signedIn: false });
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/');
  await expect(page.locator('.r-write .r-ticket')).toBeVisible();
  await expect(page.locator('.mobile-compose-toggle')).toHaveCount(0);
  for (const width of [390, 680, 900, 1280, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByRole('textbox', { name: '金额（元）' })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
});

test('receipt navigation keeps its appearance through filing and uses an inset hover surface', async ({
  page,
}) => {
  await prepare(page);
  await page.route('**/api/v1/order/list?**', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: {
          items: [0, 1].map((index) => ({
            id: `previous-${index}`,
            nickname: `先前记录${index}`,
            avatar_url: '',
            amount_fen: 100,
            message: '原来的收据',
            province: '广东',
            date: paidOrder.paid_at,
            status: 'paid',
            is_mine: false,
          })),
          next_cursor: '',
        },
      },
    }),
  );
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  const next = page.locator('.r-page-nav button').last();
  await expect(ticket).toHaveAttribute('data-payment-phase', 'confirmed');
  await expect(next).toBeEnabled();
  await expect(next).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(next).toHaveCSS('opacity', '1');
  const initial = await next.evaluate((element) => ({
    opacity: getComputedStyle(element).opacity,
    background: getComputedStyle(element).backgroundColor,
  }));
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  const filing = await next.evaluate((element) => ({
    opacity: getComputedStyle(element).opacity,
    background: getComputedStyle(element).backgroundColor,
  }));
  expect(filing.opacity).toBe(initial.opacity);
  expect(filing.background).toBe(initial.background);
  await next.evaluate((element: HTMLButtonElement) => element.click());
  await expect(
    page.locator('.receipt-destination > .r-stack:not(.payment-arrival-stack) .r-featured:not(.r-leaving)'),
  ).toContainText('先前记录0');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete', { timeout: 8000 });
  await next.hover();
  const surface = await next.evaluate((element) => ({
    width: element.getBoundingClientRect().width,
    height: element.getBoundingClientRect().height,
    innerWidth: Number.parseFloat(getComputedStyle(element, '::before').width),
    innerHeight: Number.parseFloat(getComputedStyle(element, '::before').height),
    background: getComputedStyle(element).backgroundColor,
  }));
  expect(surface.width).toBeGreaterThanOrEqual(44);
  expect(surface.height).toBeGreaterThanOrEqual(44);
  expect(surface.innerWidth).toBeLessThanOrEqual(surface.width - 10);
  expect(surface.innerHeight).toBeLessThanOrEqual(surface.height - 10);
  expect(surface.background).toBe('rgba(0, 0, 0, 0)');
});

test('a verified payment pauses for a second, slowly files the receipt and restores a fresh composer', async ({
  page,
}, info) => {
  const gateway = await prepare(page, { status: 'pending' });
  await page.clock.setFixedTime(new Date('2031-06-01T12:00:00+08:00'));
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  const pay = ticket.locator('.r-pay > .r-primary');
  await expect(pay).toBeDisabled();
  await expect(pay).toHaveText('正在查询支付');
  gateway.paid();
  await expect(ticket).toHaveAttribute('data-payment-phase', 'confirmed', { timeout: 7000 });
  const confirmedAt = Date.now();
  await expect(pay).toHaveText('查询成功');
  await page.screenshot({ path: info.outputPath('confirmed.png') });
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  await expect(pay).toHaveText('收据归档中');
  expect(Date.now() - confirmedAt).toBeGreaterThanOrEqual(750);
  const travelAt = Date.now();
  await page.screenshot({ path: info.outputPath('folding.png') });
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete', { timeout: 8000 });
  expect(Date.now() - travelAt).toBeGreaterThan(2200);
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('这张收据会归入记录');
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('广东');
  await expect(pay).toBeEnabled();
  await expect(ticket.locator('textarea')).toHaveValue('');
  await expect(ticket.getByRole('textbox', { name: '金额（元）', exact: true })).toHaveValue('20.31');
  await expect(ticket.getByRole('button', { name: '20.31', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page).toHaveURL('/');
  await page.screenshot({ path: info.outputPath('filed.png') });
  await page.reload();
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'idle');
});

test('a slow label transition cannot skip confirmed payment before its full visible second', async ({
  page,
}) => {
  await prepare(page);
  await page.addInitScript(() => {
    // Exercise real browser animations with a slow exit, independently of the status timer.
    const animate = Element.prototype.animate;
    let slowPaymentLabel = true;
    document.addEventListener('resume-payment-labels', () => {
      slowPaymentLabel = false;
    });
    Element.prototype.animate = function (...args) {
      const animation = animate.apply(this, args);
      if (this.classList.contains('payment-button-label')) {
        if (slowPaymentLabel) animation.playbackRate = 0.05;
        animation.addEventListener(
          'finish',
          () => {
            if (this.textContent?.includes('查询成功') && getComputedStyle(this).opacity === '1')
              document.dispatchEvent(new Event('payment-confirmation-entered'));
          },
          { once: true },
        );
      }
      return animation;
    };
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  const label = ticket.locator('.payment-button-label');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'confirmed');
  await expect
    .poll(() =>
      label.evaluate((node) => node.getAnimations().some((animation) => animation.playbackRate === 0.05)),
    )
    .toBe(true);
  await page.waitForTimeout(1300);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'confirmed');
  const visibleDuration = await page.evaluate(() => {
    const ticket = document.querySelector<HTMLElement>('.r-write .r-ticket');
    if (!ticket) throw new Error('Payment ticket is missing');
    const elapsed = new Promise<number>((resolve, reject) => {
      let visibleAt: number | undefined;
      const entered = () => {
        visibleAt ??= performance.now();
      };
      document.addEventListener('payment-confirmation-entered', entered);
      const observer = new MutationObserver(() => {
        if (ticket.dataset.paymentPhase === 'filing' || ticket.dataset.paymentPhase === 'complete') {
          observer.disconnect();
          document.removeEventListener('payment-confirmation-entered', entered);
          if (visibleAt === undefined) reject(new Error('Confirmed label was skipped'));
          else resolve(performance.now() - visibleAt);
        }
      });
      observer.observe(ticket, { attributes: true, attributeFilter: ['data-payment-phase'] });
    });
    document.dispatchEvent(new Event('resume-payment-labels'));
    for (const animation of document.getAnimations()) {
      if (
        animation.effect instanceof KeyframeEffect &&
        animation.effect.target instanceof Element &&
        animation.effect.target.classList.contains('payment-button-label')
      )
        animation.finish();
    }
    return elapsed;
  });
  // Native animation and DOM events share the page clock; allow only timer rounding.
  expect(visibleDuration).toBeGreaterThanOrEqual(999);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete', { timeout: 8000 });
});

test('the same opaque paper lands on the receipt before the paid row is revealed or refetched', async ({
  page,
}, info) => {
  await prepare(page);
  let listRequests = 0;
  const record = {
    id: orderID,
    is_mine: true,
    nickname: paidOrder.nickname,
    avatar_url: '',
    amount_fen: 888,
    message: paidOrder.message,
    province: '广东',
    status: 'paid',
    date: paidOrder.paid_at,
  };
  await page.route('**/api/v1/order/list?**', (route) => {
    listRequests++;
    return route.fulfill({
      json: {
        code: 'OK',
        data: {
          items: [
            record,
            {
              ...record,
              id: 'previous-order',
              is_mine: false,
              nickname: '先前捐赠者',
              message: '原来的收据',
            },
          ],
          next_cursor: '',
        },
      },
    });
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'confirmed');
  await expect(page.locator('.r-index-row')).toHaveCount(1);
  await expect(page.locator('.r-featured:not(.r-leaving):not(.payment-arrival)')).toContainText('原来的收据');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  const requestsBeforeLanding = listRequests;
  const paper = page.locator('.payment-transfer-paper');
  await expect(paper).toBeVisible();
  const colors = await paper.evaluate((element) => ({
    moving: getComputedStyle(element).backgroundColor,
    destination: getComputedStyle(document.querySelector('.payment-arrival') as Element).backgroundColor,
  }));
  expect(colors.moving).not.toBe('rgba(0, 0, 0, 0)');
  expect(colors.moving).toBe(colors.destination);
  const grids = await paper.evaluate((element) => ({
    source: getComputedStyle(document.querySelector('.r-write .r-presets') as Element).gridTemplateColumns,
    moving: getComputedStyle(element.querySelector('.r-presets') as Element).gridTemplateColumns,
  }));
  const sourceColumns = grids.source.split(' ').map(Number.parseFloat);
  const movingColumns = grids.moving.split(' ').map(Number.parseFloat);
  expect(movingColumns).toHaveLength(sourceColumns.length);
  for (let index = 0; index < sourceColumns.length; index++)
    expect(Math.abs(movingColumns[index] - sourceColumns[index])).toBeLessThan(0.1);
  const sampling = page.evaluate(async () => {
    const values: {
      opacity: number;
      width: number;
      left: number;
      top: number;
      height: number;
      gap: number;
    }[] = [];
    while (document.querySelector('.r-write .r-ticket')?.getAttribute('data-payment-phase') === 'filing') {
      const moving = document.querySelector('.payment-transfer-paper');
      const arrival = document.querySelector('.payment-arrival');
      if (moving && arrival) {
        const a = moving.getBoundingClientRect(),
          b = arrival.getBoundingClientRect();
        values.push({
          opacity: Number(getComputedStyle(moving).opacity),
          width: a.width,
          left: a.left,
          top: a.top,
          height: a.height,
          gap: Math.max(
            Math.abs(a.left - b.left),
            Math.abs(a.top - b.top),
            Math.abs(a.width - b.width),
            Math.abs(a.height - b.height),
          ),
        });
        if (values.length === 20) document.dispatchEvent(new Event('visibilitychange'));
      }
      await new Promise(requestAnimationFrame);
    }
    return values;
  });
  await expect
    .poll(() => paper.evaluate((element) => Number(element.getAnimations()[0]?.currentTime ?? 0)))
    .toBeGreaterThan(1500);
  await page.screenshot({ path: info.outputPath('morph-midway.png') });
  await expect
    .poll(() => paper.evaluate((element) => Number(element.getAnimations()[0]?.currentTime ?? 0)))
    .toBeGreaterThan(3000);
  await page.screenshot({ path: info.outputPath('morph-arriving.png') });
  const samples = await sampling;
  expect(samples.length).toBeGreaterThan(40);
  expect(samples.every((frame) => frame.opacity === 1 && frame.width > 200)).toBe(true);
  expect(samples.at(-1)?.gap).toBeLessThan(1.5);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-index-row')).toHaveCount(2);
  expect(listRequests).toBe(requestsBeforeLanding + 1);
  await expect(page.locator('.payment-transfer-paper')).toHaveCount(0);
  const landed = page.locator('.r-featured:not(.r-leaving)');
  await expect(landed).toContainText(paidOrder.message);
  const actual = await landed.boundingBox();
  const last = samples.at(-1);
  expect(actual).not.toBeNull();
  expect(last).toBeDefined();
  for (const key of ['left', 'top', 'width', 'height'] as const) {
    const observed = key === 'left' ? actual?.x : key === 'top' ? actual?.y : actual?.[key];
    expect(Math.abs((last?.[key] ?? -1000) - (observed ?? 1000))).toBeLessThan(2);
  }
  const state = await landed.evaluate((element) => ({
    opacity: getComputedStyle(element).opacity,
    animations: element.getAnimations().length,
  }));
  expect(state.opacity).toBe('1');
  await expect(ticket).toHaveCSS('opacity', '1');
});

for (const width of [1280, 390]) {
  test(`the landed payment smoothly expands its new index row without remounting existing rows at ${width}px`, async ({
    page,
  }) => {
    await prepare(page);
    await page.setViewportSize({ width, height: 1000 });
    await page.route('**/api/v1/order/list?**', (route) =>
      route.fulfill({
        json: {
          code: 'OK',
          data: {
            items: [
              {
                id: orderID,
                is_mine: true,
                nickname: paidOrder.nickname,
                avatar_url: '',
                amount_fen: paidOrder.amount_fen,
                message: paidOrder.message,
                province: paidOrder.province,
                status: 'paid',
                date: paidOrder.paid_at,
              },
              {
                id: 'previous-order',
                is_mine: false,
                nickname: '先前捐赠者',
                avatar_url: '',
                amount_fen: 100,
                message: '原来的收据',
                province: '广东',
                status: 'paid',
                date: paidOrder.created_at,
              },
            ],
            next_cursor: '',
          },
        },
      }),
    );
    await page.goto(`/?payment=returned&order=${orderID}`);
    const ticket = page.locator('.r-write .r-ticket');
    await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
    await expect(page.locator('.r-index-row')).toHaveCount(1);
    const frames = await page.evaluate(async () => {
      const index = document.querySelector('.r-index');
      const original = index?.querySelector('.r-index-row');
      if (!index || !original) throw new Error('Previous donation row is missing');
      const frames: {
        phase: string;
        height: number;
        opacity: number | null;
        connected: boolean;
        oldOpacity: string;
      }[] = [];
      let completedAt: number | undefined;
      const started = performance.now();
      while (performance.now() - started < 12000) {
        const phase = document.querySelector('.r-write .r-ticket')?.getAttribute('data-payment-phase') ?? '';
        const inserted = Array.from(index.querySelectorAll('.r-index-row')).find((row) =>
          row.textContent?.includes('付款测试'),
        );
        if (phase === 'complete') completedAt ??= performance.now();
        frames.push({
          phase,
          height: index.getBoundingClientRect().height,
          opacity: inserted ? Number(getComputedStyle(inserted).opacity) : null,
          connected: original.isConnected,
          oldOpacity: getComputedStyle(original).opacity,
        });
        if (completedAt !== undefined && performance.now() - completedAt > 1100) return frames;
        await new Promise(requestAnimationFrame);
      }
      throw new Error('Receipt did not land while sampling the index');
    });
    const filing = frames.filter((frame) => frame.phase === 'filing');
    const landed = frames.filter((frame) => frame.phase === 'complete');
    expect(filing.length).toBeGreaterThan(0);
    expect(filing.every((frame) => frame.opacity === null)).toBe(true);
    expect(
      landed.some((frame) => frame.opacity !== null && frame.opacity > 0.02 && frame.opacity < 0.98),
    ).toBe(true);
    expect(frames.every((frame) => frame.connected && frame.oldOpacity === '1')).toBe(true);
    const before = filing.at(-1)?.height ?? 0;
    const after = landed.at(-1)?.height ?? 0;
    expect(after - before).toBeGreaterThan(30);
    expect(landed.some((frame) => frame.height > before + 1 && frame.height < after - 1)).toBe(true);
    await expect(page.locator('.r-index-row')).toHaveCount(2);
    await expect(page.locator('.r-index-row').first()).toHaveCSS('opacity', '1');
  });
}

test('pending payment is not declared successful from return parameters and permits a later check', async ({
  page,
}) => {
  const gateway = await prepare(page, { status: 'pending' });
  await page.goto(`/?payment=returned&order=${orderID}&trade_status=TRADE_SUCCESS`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'checking');
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  await expect(page.locator('.r-featured')).toHaveCount(0);
  await page.getByRole('button', { name: '稍后查看', exact: true }).click();
  await expect(page.locator('.r-pay > .r-primary')).toBeEnabled();
  await expect(page.locator('.r-message')).toHaveValue('保留的草稿');
  gateway.paid();
});

test('an unavailable order offers recovery instead of success or duplicate payment', async ({ page }) => {
  const gateway = await prepare(page, { error: 'NOT_FOUND' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-pay > .r-primary')).toContainText('不存在');
  await expect(page.locator('.payment-return-status p')).toHaveCount(0);
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  gateway.recover();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.getByRole('button', { name: '重新查询', exact: true }).click();
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('8.88');
});

test('leaving a failed payment fades the button label and footer while the composer changes height', async ({
  page,
}) => {
  await prepare(page, { error: 'NOT_FOUND' });
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'error');
  const samples = page.evaluate(async () => {
    const frames: { phase: string | null; label: number[]; footer: number[]; height: number }[] = [];
    for (let index = 0; index < 55; index++) {
      await new Promise(requestAnimationFrame);
      const card = document.querySelector('.r-write .r-ticket');
      frames.push({
        phase: card?.getAttribute('data-payment-phase') ?? null,
        label: Array.from(document.querySelectorAll('.payment-button-label')).map((node) =>
          Number(getComputedStyle(node).opacity),
        ),
        footer: Array.from(document.querySelectorAll('.payment-footer-content')).map((node) =>
          Number(getComputedStyle(node).opacity),
        ),
        height: card?.getBoundingClientRect().height ?? 0,
      });
    }
    return frames;
  });
  await page.getByRole('button', { name: '稍后查看', exact: true }).click();
  const frames = (await samples).filter((frame) => frame.phase === 'idle');
  expect(frames.some((frame) => frame.label.some((opacity) => opacity > 0.02 && opacity < 0.98))).toBe(true);
  expect(frames.some((frame) => frame.footer.some((opacity) => opacity > 0.02 && opacity < 0.98))).toBe(true);
  await expect(ticket.locator('.r-ticket-foot')).toBeVisible();
  await expect(ticket.locator('.payment-return-status')).toHaveCount(0);
  await expect(ticket.locator('.r-primary')).toBeEnabled();
  await expect(ticket.locator('.r-channel')).toBeEnabled();
  const heights = frames.map((frame) => frame.height);
  const min = Math.min(...heights);
  const max = Math.max(...heights);
  expect(max - min).toBeGreaterThan(1);
  expect(heights.some((height) => height > min + 0.25 && height < max - 0.25)).toBe(true);
});

test('an invalid amount disables both payment actions and cannot open the channel menu', async ({ page }) => {
  await prepare(page);
  await page.goto('/');
  const amount = page.getByRole('textbox', { name: '金额（元）', exact: true });
  const channel = page.getByRole('button', { name: '支付方式', exact: true });
  await amount.fill('0');
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  await expect(channel).toBeDisabled();
  await channel.dispatchEvent('pointerdown');
  await expect(page.getByRole('menu', { name: '支付方式' })).toHaveCount(0);
  await amount.fill('8.88');
  await expect(channel).toBeEnabled();
  await channel.click();
  await expect(page.getByRole('menuitem', { name: '微信', exact: true })).toBeVisible();
});

test('a signed-out return requests sign-in without querying private order data', async ({ page }) => {
  await prepare(page, { signedIn: false });
  const requests: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/order/detail')) requests.push(request.url());
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-pay > .r-primary')).toContainText('登录');
  await expect(page.locator('.r-pay > .r-primary')).toBeDisabled();
  await page.locator('.payment-return-status').getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '登录', exact: true })).toBeVisible();
  expect(requests).toEqual([]);
});

test('a narrow-screen reduced-motion return completes without a flying paper or horizontal overflow', async ({
  page,
}) => {
  await prepare(page);
  await page.setViewportSize({ width: 320, height: 720 });
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('8.88');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.locator('.r-pay > .r-primary')).toBeEnabled();
});

test('automatic checking stops with an actionable pending state instead of disabling forever', async ({
  page,
}) => {
  await prepare(page, { status: 'pending' });
  await page.clock.install();
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'checking');
  await page.clock.fastForward(46000);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'pending');
  await expect(page.getByRole('button', { name: '重新查询', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '稍后查看', exact: true }).click();
  await expect(page.locator('.r-pay > .r-primary')).toBeEnabled();
});

test('a readable payment owned by somebody else cannot become a personal success receipt', async ({
  page,
}) => {
  await prepare(page);
  await page.route('**/api/v1/order/detail?**', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: { ...paidOrder, user_id: 'another-user' },
      },
    }),
  );
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'error');
  await expect(page.locator('.r-pay > .r-primary')).toContainText('无权访问');
  await expect(page.locator('.r-featured')).toHaveCount(0);
});

test('a phone receipt follows the document to the record without creating sideways scroll', async ({
  page,
}, info) => {
  await prepare(page);
  await page.setViewportSize({ width: 320, height: 720 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  const ticket = page.locator('.r-write .r-ticket');
  await expect(ticket).toHaveAttribute('data-payment-phase', 'filing');
  const samples = await page.evaluate(async () => {
    const frames: { overflow: number; top: number; transform: string; documentTop: number }[] = [];
    while (
      document.querySelector('.r-write .r-ticket')?.getAttribute('data-payment-phase') === 'filing' &&
      frames.length < 300
    ) {
      const el = document.querySelector('.payment-transfer-paper');
      if (!el) break;
      frames.push({
        overflow: document.documentElement.scrollWidth - innerWidth,
        top: el.getBoundingClientRect().top,
        transform: getComputedStyle(el).transform,
        documentTop: el.getBoundingClientRect().top + scrollY,
      });
      await new Promise(requestAnimationFrame);
    }
    const settled = document.querySelector('.r-featured:not(.r-leaving):not(.payment-arrival)');
    return { frames, firstLandedTop: settled ? settled.getBoundingClientRect().top + scrollY : -1000 };
  });
  expect(samples.frames.length).toBeGreaterThan(30);
  expect(Math.max(...samples.frames.map((frame) => frame.overflow))).toBeLessThanOrEqual(0);
  expect(new Set(samples.frames.map((frame) => frame.transform)).size).toBeGreaterThan(20);
  expect(Math.abs(samples.firstLandedTop - (samples.frames.at(-1)?.documentTop ?? -1000))).toBeLessThan(2);
  await expect(ticket).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('这张收据会归入记录');
  await page.screenshot({ path: info.outputPath('phone-filed-dark.png') });
});

test('fresh server records replace the temporary receipt snapshot after payment', async ({ page }) => {
  await prepare(page);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured')).toContainText('这张收据会归入记录');
  await page.route('**/api/v1/order/list?**', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: {
          items: [
            {
              id: orderID,
              is_mine: true,
              nickname: '更新的昵称',
              avatar_url: '',
              amount_fen: 888,
              message: '更新后的留言',
              province: '上海',
              status: 'paid',
              date: '2026-10-30T08:01:00Z',
            },
          ],
          next_cursor: '',
        },
      },
    }),
  );
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('更新后的留言');
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText('上海');
});

test('authoritative empty records do not resurrect a delivered payment after deletion', async ({ page }) => {
  await prepare(page);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  let removed = false;
  let requests = 0;
  await page.route('**/api/v1/order/list?**', (route) => {
    requests++;
    return route.fulfill({
      json: {
        code: 'OK',
        data: {
          items: removed ? [] : [{ ...paidOrder, date: paidOrder.paid_at, is_mine: true }],
          next_cursor: '',
        },
      },
    });
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect(page.locator('.r-featured:not(.r-leaving)')).toContainText(paidOrder.message);
  await expect.poll(() => requests).toBeGreaterThanOrEqual(2);
  removed = true;
  await page.getByRole('button', { name: '只看我的', exact: true }).click();
  await expect.poll(() => requests).toBeGreaterThanOrEqual(3);
  await expect(page.locator('.r-book .r-empty')).toBeVisible();
  await expect(page.locator('.r-featured:not(.r-leaving)')).toHaveCount(0);
  await expect(page.locator('.r-book')).not.toContainText(paidOrder.message);
});

test('a successful empty refresh retires the temporary payment snapshot', async ({ page }) => {
  await prepare(page);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  let requests = 0;
  await page.route('**/api/v1/order/list?**', (route) => {
    requests++;
    return route.fulfill({ json: { code: 'OK', data: { items: [], next_cursor: '' } } });
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await expect(page.locator('.r-write .r-ticket')).toHaveAttribute('data-payment-phase', 'complete');
  await expect.poll(() => requests).toBeGreaterThanOrEqual(2);
  await expect(page.locator('.r-book .r-empty')).toBeVisible();
  await expect(page.locator('.r-index-row')).toHaveCount(0);
});

test('retrying a failed OAuth authorization preserves the payment return destination', async ({ page }) => {
  await prepare(page, { signedIn: false });
  const destinations: string[] = [];
  await page.route('**/api/v1/user/login/status', (route) =>
    route.fulfill({
      json: {
        code: 'OK',
        data: {
          stage: 'processing',
          provider: 'oidc',
          return_to: `/?payment=returned&order=${orderID}`,
        },
      },
    }),
  );
  await page.route('**/api/v1/user/login', (route) => {
    destinations.push(route.request().postDataJSON().return_to);
    return route.fulfill({
      json: { code: 'OK', data: { authorization_url: '/auth/complete?error=authorization' } },
    });
  });
  await page.goto(`/?payment=returned&order=${orderID}`);
  await page.locator('.payment-return-status').getByRole('button', { name: '登录', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '登录', exact: true });
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect(page).toHaveURL('/auth/complete?error=authorization');
  await page.locator('main').getByRole('button', { name: '登录', exact: true }).click();
  await dialog.getByRole('button', { name: 'OIDC', exact: true }).click();
  await expect.poll(() => destinations.length).toBe(2);
  expect(destinations).toEqual([
    `/?payment=returned&order=${orderID}`,
    `/?payment=returned&order=${orderID}`,
  ]);
});
