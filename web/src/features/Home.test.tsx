import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import type { Order, RecordItem, Records } from '../lib/types';
import { Home } from './Home';

const app = vi.hoisted(() => ({
  profile: { id: 'receipt-owner' } as { id: string } | null,
  loading: false,
  lang: 'en',
  site: { payment_enabled: true },
  report: vi.fn(),
  setLoginOpen: vi.fn(),
}));
vi.mock('../app/context', () => ({
  useApp: () => ({ ...app, t: (key: string) => translate(key, 'en') }),
}));
vi.mock('motion/react', async (original) => ({
  ...(await original<typeof import('motion/react')>()),
  useReducedMotion: () => true,
}));
// The animation boundary exposes its landing callback; Home, payment verification,
// receipts, API decoding and the query observer all remain real.
const transfer = vi.hoisted(() => ({ active: false, complete: () => {} }));
vi.mock('./usePaymentTransfer', () => ({
  usePaymentTransfer: (props: { active: boolean; onComplete: () => void }) => {
    transfer.active = props.active;
    transfer.complete = props.onComplete;
  },
}));

const order: Order = {
  id: '68f15e68-143e-4b30-a2a9-87c0df4c5479',
  user_id: 'receipt-owner',
  nickname: 'Confirmed donor',
  avatar_url: '',
  amount_fen: 888,
  channel: 'alipay',
  status: 'paid',
  message: 'Confirmed receipt message',
  province: '广东',
  created_at: '2026-10-30T08:00:00Z',
  paid_at: '2026-10-30T08:01:00Z',
  expires_at: '2099-10-30T08:30:00Z',
  merchant_order_no: 'receipt-merchant',
  trade_no: 'receipt-trade',
  api_trade_no: '',
  protocol: 'v2',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
  remote_close_status: '',
};
const pending: RecordItem = {
  id: order.id,
  is_mine: true,
  nickname: 'Cached pending donor',
  avatar_url: '',
  amount_fen: 888,
  status: 'pending',
  message: 'Old pending message',
  province: '广东',
  date: order.created_at,
};
const paid: RecordItem = {
  ...pending,
  status: 'paid',
  nickname: 'Refreshed donor',
  message: 'Authoritative receipt message',
  date: order.paid_at ?? order.created_at,
};
const response = (data: unknown) => Response.json({ code: 'OK', data });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
const clients: QueryClient[] = [];
const advance = async (ms = 0) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};
beforeEach(() => {
  vi.useFakeTimers();
  onlineManager.setOnline(true);
  app.profile = { id: order.user_id };
  app.loading = false;
  sessionStorage.clear();
  history.replaceState(null, '', `/?payment=returned&order=${order.id}`);
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.stubGlobal('scrollTo', vi.fn());
});
afterEach(async () => {
  cleanup();
  for (const client of clients.splice(0)) {
    await client.cancelQueries();
    client.clear();
  }
  onlineManager.setOnline(true);
  vi.useRealTimers();
  vi.unstubAllGlobals();
  history.replaceState(null, '', '/');
});
function renderHome(
  list: (path: string) => Promise<Response>,
  mineItems: RecordItem[] = [],
  { cached = true } = {},
) {
  const listRequests: string[] = [];
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  clients.push(client);
  const seed = (owner: string, mine: boolean, items: RecordItem[] = []) =>
    client.setQueryData(['records', owner, mine], {
      pages: [{ items, next_cursor: '' }],
      pageParams: [''],
    });
  if (cached) seed(order.user_id, false);
  seed(order.user_id, true, mineItems);
  vi.stubGlobal('fetch', (input: string) => {
    const path = String(input);
    if (path.includes('/order/detail?')) return Promise.resolve(response(order));
    if (path.includes('/order/list?')) {
      listRequests.push(path);
      return list(path);
    }
    throw new Error(`Unexpected request: ${path}`);
  });
  const node = () => (
    <QueryClientProvider client={client}>
      <div className="d-app">
        <Home />
      </div>
    </QueryClientProvider>
  );
  const view = render(node());
  return { ...view, client, seed, listRequests, rerenderHome: () => view.rerender(node()) };
}
function receipt(container: HTMLElement) {
  return container.querySelector('.receipt-destination > .r-stack:not(.payment-arrival-stack) .r-featured');
}
function expectConfirmed(container: HTMLElement) {
  const paper = receipt(container);
  expect(paper).toBeInTheDocument();
  expect(paper).toHaveTextContent('Confirmed receipt message');
  expect(paper).not.toHaveTextContent('Old pending message');
  expect(within(paper as HTMLElement).queryByRole('button', { name: 'Continue payment' })).toBeNull();
  return paper;
}
async function land() {
  expect(transfer.active).toBe(true);
  act(() => transfer.complete());
  await advance();
}
async function confirm() {
  await advance(1000);
  await advance(1000);
}

it('keeps the paid receipt when the landing refresh is paused offline until a real success', async () => {
  const refresh = deferred<Response>();
  const view = renderHome(() => refresh.promise);
  await confirm();
  expect(view.listRequests).toEqual([]);
  expect(view.client.getQueryState(['records', order.user_id, false])?.fetchStatus).toBe('idle');
  onlineManager.setOnline(false);
  await land();
  expect(view.client.getQueryState(['records', order.user_id, false])).toMatchObject({
    status: 'success',
    fetchStatus: 'paused',
  });
  expect(view.listRequests).toEqual([]);
  expectConfirmed(view.container);
  // A cache edit is another old success state, not a server acknowledgment.
  act(() => view.seed(order.user_id, false));
  await advance();
  expectConfirmed(view.container);
  act(() => onlineManager.setOnline(true));
  await advance();
  await act(async () => refresh.resolve(response({ items: [], next_cursor: '' })));
  await advance();
  expect(receipt(view.container)).toBeNull();
});

it.each(['checking', 'confirmed'])(
  'prefers the confirmed paid snapshot over the cached pending row after switching mine during %s and a 503',
  async (phase) => {
    const refresh = deferred<Response>();
    const view = renderHome(() => refresh.promise, [pending]);
    if (phase === 'confirmed') await advance(1000);
    fireEvent.click(screen.getByRole('button', { name: 'Only mine' }));
    if (phase === 'checking') await advance(1000);
    await advance(1000);
    expect(receipt(view.container)).toBeNull();
    expect(view.listRequests).toEqual([]);
    await land();
    const paper = expectConfirmed(view.container);
    await act(async () => refresh.resolve(new Response('unavailable', { status: 503 })));
    await advance();
    expectConfirmed(view.container);
    expect(receipt(view.container)).toBe(paper);
  },
);

it.each([
  ['a fresh paid row', [paid]],
  ['a legitimate empty result', []],
])('retires the snapshot after a successful list refresh with %s', async (_name, items) => {
  const refresh = deferred<Response>();
  const view = renderHome(() => refresh.promise);
  await confirm();
  await land();
  const paper = expectConfirmed(view.container);
  await act(async () => refresh.resolve(response({ items, next_cursor: '' } satisfies Records)));
  await advance();
  expect(view.container).not.toHaveTextContent('Confirmed receipt message');
  if (items.length) {
    expect(receipt(view.container)).toHaveTextContent('Authoritative receipt message');
    expect(receipt(view.container)).toBe(paper);
  } else expect(receipt(view.container)).toBeNull();
});

it('uses a later successful retry after a failed landing refresh as authority', async () => {
  let fail = true;
  const view = renderHome(async () =>
    fail ? new Response('unavailable', { status: 503 }) : response({ items: [], next_cursor: '' }),
  );
  await confirm();
  await land();
  expectConfirmed(view.container);
  fail = false;
  await act(async () => {
    await view.client.refetchQueries({ queryKey: ['records', order.user_id, false], exact: true });
  });
  await advance();
  expect(receipt(view.container)).toBeNull();
});

it.each(['success', 'failure'])(
  'does not revive the paid snapshot when changing filters before a late refresh %s',
  async (outcome) => {
    const refresh = deferred<Response>();
    const view = renderHome(() => refresh.promise, [pending]);
    await confirm();
    await land();
    expectConfirmed(view.container);
    fireEvent.click(screen.getByRole('button', { name: 'Only mine' }));
    await advance();
    expect(receipt(view.container)).toHaveTextContent('Old pending message');
    await act(async () =>
      refresh.resolve(
        outcome === 'success'
          ? response({ items: [], next_cursor: '' })
          : new Response('unavailable', { status: 503 }),
      ),
    );
    await advance();
    fireEvent.click(screen.getByRole('button', { name: 'Only mine' }));
    await advance();
    expect(receipt(view.container)).toBeNull();
  },
);

it.each(['success', 'failure'])(
  'does not revive a prior account snapshot after switching identities during a late refresh %s',
  async (outcome) => {
    const refresh = deferred<Response>();
    const view = renderHome(() => refresh.promise);
    await confirm();
    await land();
    expectConfirmed(view.container);
    view.seed('another-owner', false);
    app.profile = { id: 'another-owner' };
    view.rerenderHome();
    await advance();
    expect(receipt(view.container)).toBeNull();
    await act(async () =>
      refresh.resolve(
        outcome === 'success'
          ? response({ items: [], next_cursor: '' })
          : new Response('unavailable', { status: 503 }),
      ),
    );
    await advance();
    app.profile = { id: order.user_id };
    view.rerenderHome();
    await advance();
    expect(receipt(view.container)).toBeNull();
  },
);

it('does not retire the bridge when a next-page success cancels the paused landing refresh', async () => {
  const next = deferred<Response>();
  const view = renderHome((path) =>
    path.includes('cursor=page-two')
      ? next.promise
      : Promise.resolve(response({ items: [], next_cursor: '' })),
  );
  view.client.setQueryData(['records', order.user_id, false], {
    pages: [{ items: [], next_cursor: 'page-two' }],
    pageParams: [''],
  });
  await confirm();
  onlineManager.setOnline(false);
  await land();
  const paper = expectConfirmed(view.container);
  fireEvent.click(screen.getByRole('button', { name: 'More' }));
  act(() => onlineManager.setOnline(true));
  await advance();
  await act(async () => next.resolve(response({ items: [], next_cursor: '' })));
  await advance();
  expectConfirmed(view.container);
  expect(receipt(view.container)).toBe(paper);
  await act(async () => {
    await view.client.refetchQueries({ queryKey: ['records', order.user_id, false], exact: true });
  });
  await advance();
  expect(receipt(view.container)).toBeNull();
});

it('does not restore a local receipt after unmounting before a late list success', async () => {
  const refresh = deferred<Response>();
  const view = renderHome(() => refresh.promise);
  await confirm();
  await land();
  expectConfirmed(view.container);
  view.unmount();
  await act(async () => refresh.resolve(response({ items: [], next_cursor: '' })));
  await advance();
  const next = render(
    <QueryClientProvider client={view.client}>
      <div className="d-app">
        <Home />
      </div>
    </QueryClientProvider>,
  );
  await advance();
  expect(receipt(next.container)).toBeNull();
  expect(next.container).not.toHaveTextContent('Confirmed receipt message');
});

it.each([
  ['a fresh paid row', [paid]],
  ['a legitimate empty result', []],
])(
  'ignores the uncached pre-landing response until a post-landing refresh returns %s',
  async (_name, items) => {
    const initial = deferred<Response>();
    const refresh = deferred<Response>();
    let requests = 0;
    const view = renderHome(() => (++requests === 1 ? initial.promise : refresh.promise), [], {
      cached: false,
    });
    await confirm();
    expect(view.listRequests).toHaveLength(1);
    await land();
    const paper = expectConfirmed(view.container);
    await act(async () => initial.resolve(response({ items: [pending], next_cursor: '' })));
    await advance();
    expectConfirmed(view.container);
    expect(receipt(view.container)).toBe(paper);
    expect(view.listRequests).toHaveLength(2);
    await act(async () => refresh.resolve(response({ items, next_cursor: '' } satisfies Records)));
    await advance();
    expect(view.container).not.toHaveTextContent('Confirmed receipt message');
    if (items.length) {
      expect(receipt(view.container)).toHaveTextContent('Authoritative receipt message');
      expect(receipt(view.container)).toBe(paper);
    } else expect(receipt(view.container)).toBeNull();
  },
);

it('keeps the uncached paid snapshot after the post-landing refresh fails and an older pending response arrives', async () => {
  const initial = deferred<Response>();
  const refresh = deferred<Response>();
  let requests = 0;
  const view = renderHome(
    () => {
      requests++;
      if (requests === 1) return initial.promise;
      if (requests === 2) return refresh.promise;
      return Promise.resolve(response({ items: [], next_cursor: '' }));
    },
    [],
    { cached: false },
  );
  await confirm();
  await land();
  const paper = expectConfirmed(view.container);
  await act(async () => refresh.resolve(new Response('unavailable', { status: 503 })));
  await advance();
  expectConfirmed(view.container);
  await act(async () => initial.resolve(response({ items: [pending], next_cursor: '' })));
  await advance();
  expectConfirmed(view.container);
  expect(receipt(view.container)).toBe(paper);
  await act(async () => {
    await view.client.refetchQueries({ queryKey: ['records', order.user_id, false], exact: true });
  });
  await advance();
  expect(receipt(view.container)).toBeNull();
});

it('does not revive the uncached initial pending row after the new post-landing list removed it', async () => {
  const initial = deferred<Response>();
  const refresh = deferred<Response>();
  let requests = 0;
  const view = renderHome(() => (++requests === 1 ? initial.promise : refresh.promise), [], {
    cached: false,
  });
  await confirm();
  await land();
  expectConfirmed(view.container);
  await act(async () => refresh.resolve(response({ items: [], next_cursor: '' })));
  await advance();
  expect(receipt(view.container)).toBeNull();
  await act(async () => initial.resolve(response({ items: [pending], next_cursor: '' })));
  await advance();
  expect(receipt(view.container)).toBeNull();
});

it('does not start the uncached post-landing refresh after leaving before cancellation settles', async () => {
  const initial = deferred<Response>();
  const view = renderHome(() => initial.promise, [], { cached: false });
  await confirm();
  expect(view.listRequests).toHaveLength(1);
  act(() => transfer.complete());
  view.unmount();
  await advance();
  expect(view.listRequests).toHaveLength(1);
  await act(async () => initial.resolve(response({ items: [pending], next_cursor: '' })));
  await advance();
  expect(view.client.getQueryData(['records', order.user_id, false])).toBeUndefined();
});
