import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Order, RecordItem } from '../lib/types';
import { Home } from './Home';

function CurrentProfile() {
  const { profile } = useApp();
  return <p data-testid="current-profile">{profile?.nickname ?? 'guest'}</p>;
}

const clients: QueryClient[] = [];
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-31T00:00:00Z'));
  history.replaceState(null, '', '/');
  localStorage.clear();
  sessionStorage.clear();
  localStorage.setItem('donate-language', 'en');
  onlineManager.setOnline(true);
  vi.stubGlobal('scrollTo', vi.fn());
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
    addEventListener() {},
    removeEventListener() {},
  }));
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  onlineManager.setOnline(true);
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it.each([403, 503])('revokes the old owner IP on account change before a delayed HTTP %s', async (status) => {
  const now = Date.now();
  const alice = {
    id: 'alice',
    nickname: 'Alice',
    csrf_token: 'alice-csrf',
    expires_at: new Date(now + 30 * 86400000).toISOString(),
    bindings: [],
  };
  const bob = { ...alice, id: 'bob', nickname: 'Bob', csrf_token: 'bob-csrf' };
  let serverProfile = alice;
  const ip = '192.0.2.17';
  const order: Order = {
    id: '68f15e68-143e-4b30-a2a9-87c0df4c5479',
    user_id: alice.id,
    nickname: alice.nickname,
    avatar_url: '',
    amount_fen: 888,
    channel: 'alipay',
    status: 'paid',
    message: 'Private owner detail',
    province: '广东',
    ip,
    created_at: '2026-10-30T08:00:00Z',
    paid_at: '2026-10-30T08:01:00Z',
    expires_at: '2026-10-30T08:30:00Z',
    merchant_order_no: 'synthetic-merchant',
    trade_no: 'synthetic-trade',
    api_trade_no: '',
    protocol: 'v2',
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
    amount_fen: 888,
    status: 'paid',
    message: '',
    province: '广东',
    date: '2026-10-30T08:01:00Z',
  };
  let detailRequests = 0;
  let finishDetail: ((response: Response) => void) | undefined;
  const ok = (data: unknown) => Response.json({ code: 'OK', data });
  vi.stubGlobal('fetch', async (input: string) => {
    const path = String(input);
    if (path.endsWith('user/profile')) return ok(serverProfile);
    if (path.endsWith('site/info'))
      return ok({ csrf_token: 'guest-csrf', payment_enabled: true, providers: [] });
    if (path.includes('/order/list?'))
      return ok({ items: serverProfile.id === alice.id ? [record] : [], next_cursor: '' });
    if (path.includes('/order/detail?')) {
      if (++detailRequests === 1) return ok(order);
      return new Promise<Response>((resolve) => {
        finishDetail = resolve;
      });
    }
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
  // Match production query defaults. A reconnect, not a focus event, revalidates identity.
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: 1, staleTime: 15000, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <div className="d-app">
          <CurrentProfile />
          <Home />
        </div>
      </AppProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('current-profile')).toHaveTextContent('Alice'));
  fireEvent.click(await screen.findByRole('button', { name: 'Order details' }));
  await screen.findByText(ip);

  // Another tab changes the shared login cookie. All identity updates still use real profile GETs.
  serverProfile = bob;
  vi.setSystemTime(now + 16000);
  fireEvent(window, new Event('offline'));
  fireEvent(window, new Event('online'));
  await waitFor(() => expect(screen.getByTestId('current-profile')).toHaveTextContent('Bob'));
  await waitFor(() => expect(finishDetail).toBeTypeOf('function'));
  expect
    .soft(screen.queryByText(ip), 'a new account cannot see the previous owner IP')
    .not.toBeInTheDocument();
  await act(async () => {
    finishDetail?.(
      status === 403
        ? Response.json({ code: 'FORBIDDEN', data: null }, { status })
        : new Response('synthetic unavailable', { status }),
    );
  });
  expect(screen.queryByText(ip)).not.toBeInTheDocument();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});
