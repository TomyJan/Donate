import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Order, Profile, Site } from '../lib/types';
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
  csrf_token: 'guest-csrf',
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
  message: '',
  province: '',
  created_at: '2026-11-01T00:00:00Z',
  paid_at: null,
  expires_at: '2099-11-01T00:30:00Z',
  merchant_order_no: 'synthetic-old',
  trade_no: '',
  api_trade_no: '',
  protocol: 'v1',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  gateway_status: '',
  remote_close_status: '',
};
const nextOrder: Order = {
  ...order,
  id: '68f15e68-143e-4b30-a2a9-87c0df4c5480',
  merchant_order_no: 'synthetic-new',
};
const ok = (data: unknown) => Response.json({ code: 'OK', data });
const failure = (code: string) => Response.json({ code, data: null });
const clients: QueryClient[] = [];
function State() {
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
async function fixture(
  handle: (path: string, body: Record<string, string>, csrf: string | null) => Promise<Response> | Response,
) {
  let profile = alice;
  const submitted: string[] = [];
  vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function (this: HTMLFormElement) {
    submitted.push(this.querySelector<HTMLInputElement>('[name=order]')?.value ?? '');
  });
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const path = String(input);
    if (path.endsWith('user/profile')) return ok(profile);
    if (path.endsWith('site/info')) return ok(site);
    if (path.includes('order/list?')) return ok({ items: [], next_cursor: '' });
    return handle(
      path,
      init?.body ? JSON.parse(String(init.body)) : {},
      new Headers(init?.headers).get('X-CSRF-Token'),
    );
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <State />
        <div className="d-app">
          <Home />
        </div>
      </AppProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('alice'));
  const click = async () => {
    const button = screen.getByRole('button', { name: 'Donate · Alipay' });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    return button;
  };
  const settle = async (button: HTMLElement) => waitFor(() => expect(button).toBeEnabled());
  return {
    client,
    submitted,
    click,
    settle,
    switchAccount: (replacement = bob) => {
      profile = replacement;
      client.setQueryData(['profile'], replacement);
    },
  };
}

it.each(['closed', 'paid'] as const)(
  'rotates a confirmed %s create result only for the next explicit donation',
  async (terminal) => {
    const keys: string[] = [],
      starts: string[] = [];
    let status: Order['status'] = 'pending';
    const f = await fixture((path, body) => {
      if (path.endsWith('order/create')) {
        keys.push(body.idempotency_key);
        return ok(keys[0] === body.idempotency_key ? { ...order, status } : nextOrder);
      }
      if (path.endsWith('order/payment/start')) {
        starts.push(body.id);
        return body.id === order.id
          ? failure(starts.length === 1 ? 'PAYMENT_GATEWAY_UNAVAILABLE' : 'ORDER_NOT_PAYABLE')
          : ok({ mode: 'form', url: 'https://payment.invalid/checkout', fields: { order: body.id } });
      }
      throw new Error(`Unexpected request ${path}`);
    });
    await f.settle(await f.click());
    status = terminal;
    await f.settle(await f.click());
    expect(keys).toHaveLength(2);
    expect(keys[1]).toBe(keys[0]);
    expect(starts).toEqual([order.id]);
    expect(f.submitted).toEqual([]);
    expect(sessionStorage.getItem('donate-pending-payment')).toBeNull();
    await f.click();
    await waitFor(() => expect(f.submitted).toEqual([nextOrder.id]));
    expect(keys).toHaveLength(3);
    expect(keys[2]).not.toBe(keys[0]);
    expect(starts).toEqual([order.id, nextOrder.id]);
  },
);

it.each(['closed', 'paid'] as const)(
  'rotates after a nonpayable start is confirmed %s by order detail',
  async (terminal) => {
    const keys: string[] = [],
      starts: string[] = [];
    let details = 0;
    const f = await fixture((path, body) => {
      if (path.endsWith('order/create')) {
        keys.push(body.idempotency_key);
        return ok(keys[0] === body.idempotency_key ? order : nextOrder);
      }
      if (path.includes('order/detail?')) {
        details++;
        return ok({ ...order, status: terminal });
      }
      if (path.endsWith('order/payment/start')) {
        starts.push(body.id);
        return body.id === order.id
          ? failure(starts.length === 1 ? 'PAYMENT_GATEWAY_UNAVAILABLE' : 'ORDER_NOT_PAYABLE')
          : ok({ mode: 'form', url: 'https://payment.invalid/checkout', fields: { order: body.id } });
      }
      throw new Error(`Unexpected request ${path}`);
    });
    await f.settle(await f.click());
    await f.settle(await f.click());
    expect(keys).toHaveLength(2);
    expect(keys[1]).toBe(keys[0]);
    expect(details).toBe(1);
    expect(f.submitted).toEqual([]);
    await f.click();
    await waitFor(() => expect(f.submitted).toEqual([nextOrder.id]));
    expect(keys[2]).not.toBe(keys[0]);
  },
);

it.each([
  'create-network',
  'start-network',
  'start-error',
  'nonpayable-pending',
  'nonpayable-unknown',
] as const)('preserves the intent for %s outcomes', async (mode) => {
  const keys: string[] = [];
  let attempts = 0;
  const f = await fixture((path, body) => {
    if (path.endsWith('order/create')) {
      keys.push(body.idempotency_key);
      if (mode === 'create-network' && keys.length === 1)
        throw new TypeError('synthetic disconnected create');
      return ok(order);
    }
    if (path.includes('order/detail?')) {
      if (mode === 'nonpayable-unknown') throw new TypeError('synthetic lost detail');
      return ok(order);
    }
    if (path.endsWith('order/payment/start')) {
      if (++attempts === 1 && mode !== 'create-network') {
        if (mode === 'start-network') throw new TypeError('synthetic disconnected start');
        return failure(mode.startsWith('nonpayable') ? 'ORDER_NOT_PAYABLE' : 'PAYMENT_GATEWAY_UNAVAILABLE');
      }
      return ok({ mode: 'form', url: 'https://payment.invalid/checkout', fields: { order: body.id } });
    }
    throw new Error(`Unexpected request ${path}`);
  });
  await f.settle(await f.click());
  expect(keys).toHaveLength(1);
  expect(f.submitted).toEqual([]);
  await f.click();
  await waitFor(() => expect(f.submitted).toEqual([order.id]));
  expect(keys).toHaveLength(2);
  expect(keys[1]).toBe(keys[0]);
});

it('preserves an unknown intent when the same account receives new session credentials', async () => {
  const requests: { key: string; csrf: string | null }[] = [];
  let starts = 0;
  const f = await fixture((path, body, csrf) => {
    if (path.endsWith('order/create')) {
      requests.push({ key: body.idempotency_key, csrf });
      return ok(order);
    }
    if (path.endsWith('order/payment/start')) {
      if (++starts === 1) throw new TypeError('synthetic disconnected start');
      return ok({ mode: 'form', url: 'https://payment.invalid/checkout', fields: { order: body.id } });
    }
    throw new Error(`Unexpected request ${path}`);
  });
  await f.settle(await f.click());
  const replacement = { ...alice, csrf_token: 'alice-replacement-session' };
  await act(async () => f.switchAccount(replacement));
  await f.click();
  await waitFor(() => expect(f.submitted).toEqual([order.id]));
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual({ key: requests[0]?.key, csrf: replacement.csrf_token });
});

it('does not let an old terminal response invalidate the new account payment intent', async () => {
  let finishOld: ((response: Response) => void) | undefined;
  const requests: { key: string; csrf: string | null }[] = [];
  let bobStarts = 0;
  const f = await fixture((path, body, csrf) => {
    if (path.endsWith('order/create')) {
      requests.push({ key: body.idempotency_key, csrf });
      if (csrf === alice.csrf_token)
        return new Promise<Response>((resolve) => {
          finishOld = resolve;
        });
      return ok({ ...nextOrder, user_id: bob.id });
    }
    if (path.endsWith('order/payment/start'))
      return ++bobStarts === 1
        ? failure('PAYMENT_GATEWAY_UNAVAILABLE')
        : ok({ mode: 'form', url: 'https://payment.invalid/checkout', fields: { order: body.id } });
    throw new Error(`Unexpected request ${path}`);
  });
  await f.click();
  await waitFor(() => expect(finishOld).toBeTypeOf('function'));
  await act(async () => f.switchAccount());
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('bob'));
  await f.settle(await f.click());
  const bobKey = requests[1]?.key;
  expect(requests[1]?.csrf).toBe(bob.csrf_token);
  await act(async () => finishOld?.(ok({ ...order, status: 'closed' })));
  expect(bobStarts).toBe(1);
  await f.click();
  await waitFor(() => expect(f.submitted).toEqual([nextOrder.id]));
  expect(requests).toHaveLength(3);
  expect(requests[2]).toEqual({ key: bobKey, csrf: bob.csrf_token });
});
