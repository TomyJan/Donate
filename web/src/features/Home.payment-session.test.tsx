import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Order, Profile, RecordItem, Site } from '../lib/types';
import { Home } from './Home';

const alice: Profile = {
  id: 'alice',
  nickname: 'Alice',
  csrf_token: 'alice-session',
  expires_at: '2099-11-01T00:00:00Z',
  bindings: [],
  avatar_url: '',
  provider: 'oidc',
  admin: false,
};
const bob: Profile = { ...alice, id: 'bob', nickname: 'Bob', csrf_token: 'bob-session' };
const site: Site = {
  csrf_token: 'anonymous-session',
  logo: '',
  name: 'Synthetic site',
  payment_enabled: true,
  providers: [],
  version: { version: '0.1.1', tag: '', commit: 'synthetic', built_at: '' },
};
const order: Order = {
  id: '68f15e68-143e-4b30-a2a9-87c0df4c5479',
  user_id: alice.id,
  nickname: alice.nickname,
  avatar_url: '',
  amount_fen: 888,
  channel: 'alipay',
  status: 'pending',
  message: 'Alice payment',
  province: '广东',
  created_at: '2026-11-01T00:00:00Z',
  paid_at: null,
  expires_at: '2099-11-01T00:30:00Z',
  merchant_order_no: 'synthetic-merchant',
  trade_no: '',
  api_trade_no: '',
  protocol: 'v1',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
  remote_close_status: '',
};
const record: RecordItem = {
  id: order.id,
  is_mine: true,
  nickname: alice.nickname,
  avatar_url: '',
  amount_fen: order.amount_fen,
  status: 'pending',
  message: order.message,
  province: order.province,
  date: order.created_at,
};
const checkoutData = { mode: 'form', url: 'https://payment.invalid/checkout', fields: { order: order.id } };
const ok = (data: unknown) => Response.json({ code: 'OK', data });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function SessionState() {
  const { profile, toasts } = useApp();
  return (
    <>
      <output data-testid="session">{profile?.id ?? 'guest'}</output>
      {toasts.map((toast) => (
        <p role="alert" key={toast.id}>
          {toast.text}
        </p>
      ))}
    </>
  );
}
const clients: QueryClient[] = [];
beforeEach(() => {
  history.replaceState(null, '', '/');
  localStorage.clear();
  sessionStorage.clear();
  localStorage.setItem('donate-language', 'en');
  vi.stubGlobal('scrollTo', vi.fn());
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(async () => {
  cleanup();
  for (const client of clients.splice(0)) {
    await client.cancelQueries();
    client.clear();
  }
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

type Stage = 'create' | 'composer-checkout' | 'receipt-checkout';
async function paymentFixture(stage: Stage) {
  let serverProfile = alice;
  const pending = deferred<Response>();
  const begun = deferred<void>();
  const requests: { path: string; csrf: string | null }[] = [];
  const submitted: string[] = [];
  vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function (this: HTMLFormElement) {
    submitted.push(this.action);
  });
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const path = String(input);
    if (path.endsWith('user/profile')) return ok(serverProfile);
    if (path.endsWith('site/info')) return ok(site);
    if (path.includes('order/list?'))
      return ok({ items: serverProfile.id === alice.id ? [record] : [], next_cursor: '' });
    if (path.endsWith('order/create') || path.endsWith('order/payment/start')) {
      requests.push({ path, csrf: new Headers(init?.headers).get('X-CSRF-Token') });
      if ((stage === 'create') === path.endsWith('order/create')) {
        begun.resolve();
        return pending.promise;
      }
      return ok(path.endsWith('order/create') ? order : checkoutData);
    }
    throw new Error(`Unexpected request ${path}`);
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  const view = render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <SessionState />
        <div className="d-app">
          <Home />
        </div>
      </AppProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('alice'));
  const button = await screen.findByRole('button', {
    name: stage === 'receipt-checkout' ? 'Continue payment' : 'Donate · Alipay',
  });
  fireEvent.click(button);
  await begun.promise;
  return {
    ...view,
    client,
    pending,
    requests,
    submitted,
    switchAccount: () => {
      serverProfile = bob;
    },
  };
}

const stages: Stage[] = ['create', 'composer-checkout', 'receipt-checkout'];
it.each(stages.flatMap((stage) => [200, 401, 403].map((status) => ({ stage, status }))))(
  'revokes $stage HTTP $status before the replacement session renders',
  async ({ stage, status }) => {
    const f = await paymentFixture(stage);
    const replacement = deferred<void>();
    const unsubscribe = f.client.getQueryCache().subscribe((event) => {
      if (
        event.type !== 'updated' ||
        event.query.queryKey[0] !== 'profile' ||
        (event.query.state.data as Profile | null | undefined)?.id !== bob.id
      )
        return;
      f.pending.resolve(
        status === 200
          ? ok(stage === 'create' ? order : checkoutData)
          : Response.json({ code: status === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN', data: null }, { status }),
      );
      replacement.resolve();
    });
    f.switchAccount();
    await act(async () => {
      fireEvent.focus(window);
      await replacement.promise;
      await f.pending.promise;
    });
    unsubscribe();
    await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('bob'));
    expect(sessionStorage.getItem('donate-pending-payment')).toBeNull();
    expect(f.submitted).toEqual([]);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    if (stage === 'create')
      expect(f.requests.filter((r) => r.path.endsWith('order/payment/start'))).toEqual([]);
    expect(f.requests.every((r) => r.csrf === alice.csrf_token)).toBe(true);
    await waitFor(() => expect(f.container.querySelector('.r-pay > button')).toBeEnabled());
  },
);

it.each(stages)('still submits an active %s payment', async (stage) => {
  const f = await paymentFixture(stage);
  await act(async () => {
    f.pending.resolve(ok(stage === 'create' ? order : checkoutData));
  });
  await waitFor(() => expect(f.submitted).toEqual(['https://payment.invalid/checkout']));
  expect(JSON.parse(sessionStorage.getItem('donate-pending-payment') ?? 'null')?.id).toBe(order.id);
  expect(f.requests.every((r) => r.csrf === alice.csrf_token)).toBe(true);
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});
