import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Order, Profile } from '../lib/types';
import { usePaymentReturn } from './usePaymentReturn';

const clients: QueryClient[] = [];
const day = 86400000;
const orderID = '68f15e68-143e-4b30-a2a9-87c0df4c5479';
const ok = (data: unknown) => Response.json({ code: 'OK', data });
const advance = async (ms = 0) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};
beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-10-31T00:00:00Z'));
  onlineManager.setOnline(true);
  sessionStorage.clear();
  history.replaceState(null, '', `/?payment=returned&order=${orderID}`);
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  onlineManager.setOnline(true);
  vi.useRealTimers();
  vi.unstubAllGlobals();
  history.replaceState(null, '', '/');
});

function paymentFixture() {
  const alice: Profile = {
    id: 'alice',
    nickname: 'Alice',
    avatar_url: '',
    provider: 'qq',
    admin: false,
    bindings: [],
    csrf_token: 'alice-csrf',
    expires_at: new Date(Date.now() + 30 * day).toISOString(),
  };
  let serverProfile = alice;
  const order: Order = {
    id: orderID,
    user_id: alice.id,
    nickname: alice.nickname,
    avatar_url: '',
    amount_fen: 888,
    channel: 'alipay',
    status: 'pending',
    message: '',
    province: '广东',
    created_at: '2026-10-30T08:00:00Z',
    paid_at: null,
    expires_at: '2026-10-31T01:00:00Z',
    merchant_order_no: 'synthetic-merchant',
    trade_no: '',
    api_trade_no: '',
    protocol: 'v2',
    hidden_order: false,
    hidden_message: false,
    close_reason: '',
    gateway_status: '',
    remote_close_status: '',
  };
  let detailRequests = 0;
  let finishOld: ((response: Response) => void) | undefined;
  let oldSignal: AbortSignal | null | undefined;
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    if (path.endsWith('user/profile')) return ok(serverProfile);
    if (path.endsWith('site/info')) return ok({ csrf_token: 'guest-csrf' });
    if (path.includes('order/detail?')) {
      detailRequests++;
      if (detailRequests === 2) {
        oldSignal = init?.signal;
        return new Promise<Response>((resolve) => {
          finishOld = resolve;
        });
      }
      return ok({ ...order, user_id: serverProfile.id });
    }
    throw new Error('Unexpected synthetic request');
  });
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: 1, staleTime: 15000, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
  clients.push(client);
  const view = renderHook(() => ({ profile: useApp().profile, payment: usePaymentReturn() }), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        <AppProvider>{children}</AppProvider>
      </QueryClientProvider>
    ),
  });
  return {
    ...view,
    client,
    order,
    alice,
    detailRequests: () => detailRequests,
    oldSignal: () => oldSignal,
    finishOld: (response: Response) => {
      expect(finishOld).toBeTypeOf('function');
      finishOld?.(response);
    },
    replace: (id: string) => {
      serverProfile = { ...alice, id, csrf_token: 'replacement-csrf' };
      return serverProfile;
    },
  };
}

const denials = [
  { status: 401, code: 'UNAUTHORIZED', json: true },
  { status: 403, code: 'ACCOUNT_BANNED', json: true },
  { status: 200, code: 'ACCOUNT_BANNED', json: true },
  { status: 401, code: 'UNAUTHORIZED', json: false },
];
it.each(denials)(
  'preserves a new same-user session after old $status/$code (JSON=$json)',
  async ({ status, code, json }) => {
    const fixture = paymentFixture();
    // The profile is stale, but the second poll is only 13.5 seconds old, below the 15s timeout.
    await advance(16000);
    expect(fixture.detailRequests()).toBe(2);
    expect(fixture.oldSignal()?.aborted).toBe(false);
    const replacement = fixture.replace('alice');
    fireEvent(window, new Event('offline'));
    fireEvent(window, new Event('online'));
    await advance();
    expect(fixture.result.current.profile).toEqual(replacement);
    await act(async () => {
      fixture.finishOld(
        json ? Response.json({ code, data: null }, { status }) : new Response('expired', { status }),
      );
    });
    await advance();
    expect(fixture.client.getQueryData(['profile'])).toEqual(replacement);
    expect(fixture.result.current.profile).toEqual(replacement);
    expect(fixture.oldSignal()?.aborted).toBe(true);
    expect(fixture.detailRequests()).toBe(3);
  },
);

it('does not apply a paid response from the previous credentials', async () => {
  const fixture = paymentFixture();
  await advance(16000);
  const replacement = fixture.replace('alice');
  fireEvent(window, new Event('offline'));
  fireEvent(window, new Event('online'));
  await advance();
  expect(fixture.result.current.profile).toEqual(replacement);
  await act(async () => {
    fixture.finishOld(ok({ ...fixture.order, status: 'paid', paid_at: '2026-10-31T00:00:01Z' }));
  });
  await advance(1000);
  expect(fixture.result.current.payment.phase).toBe('checking');
  expect(fixture.result.current.payment.order).toBeUndefined();
  expect(fixture.detailRequests()).toBe(3);
});

it('guards a replacement session before its cache update reaches the payment effect', async () => {
  const fixture = paymentFixture();
  await advance(16000);
  const replacement = fixture.replace('alice');
  let releasedBeforeCleanup = false;
  const unsubscribe = fixture.client.getQueryCache().subscribe((event) => {
    if (event.type !== 'updated' || event.query.queryKey[0] !== 'profile') return;
    const value = event.query.state.data as Profile | null;
    if (!releasedBeforeCleanup && value?.csrf_token === replacement.csrf_token) {
      releasedBeforeCleanup = true;
      expect(fixture.oldSignal()?.aborted).toBe(false);
      // Query observers are batched: a new cached profile can precede React effect cleanup.
      fixture.finishOld(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
    }
  });
  try {
    fireEvent(window, new Event('offline'));
    fireEvent(window, new Event('online'));
    await advance();
    expect(releasedBeforeCleanup).toBe(true);
    expect(fixture.client.getQueryData(['profile'])).toEqual(replacement);
    expect(fixture.result.current.profile).toEqual(replacement);
  } finally {
    unsubscribe();
  }
});

it('still clears a genuinely expired current session during payment checking', async () => {
  const fixture = paymentFixture();
  await advance(16000);
  await act(async () => {
    fixture.finishOld(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
  });
  await advance();
  expect(fixture.client.getQueryData(['profile'])).toBeNull();
  expect(fixture.result.current.payment.phase).toBe('login');
});
