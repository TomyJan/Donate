import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { Order } from '../lib/types';
import { usePaymentReturn } from './usePaymentReturn';

const app = vi.hoisted(() => ({ profile: { id: 'return-user' } as { id: string } | null, loading: false }));
vi.mock('../app/context', () => ({ useApp: () => app }));

const order: Order = {
  id: '68f15e68-143e-4b30-a2a9-87c0df4c5479',
  user_id: 'return-user',
  nickname: '付款测试',
  avatar_url: '',
  amount_fen: 888,
  channel: 'alipay',
  status: 'paid',
  message: '保留收据',
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
  remote_close_status: '',
};
const response = (code = 'OK', status = 'paid') =>
  Response.json({ code, data: code === 'OK' ? { ...order, status } : null });
const advance = async (ms: number) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};

beforeEach(() => {
  vi.useFakeTimers();
  sessionStorage.clear();
  history.replaceState(null, '', `/?payment=returned&order=${order.id}`);
  app.profile = { id: order.user_id };
  app.loading = false;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  history.replaceState(null, '', '/');
});

it.each([
  ['OK', 'paid', 'confirmed'],
  ['OK', 'closed', 'pending'],
  ['ORDER_NOT_FOUND', 'paid', 'error'],
  ['UNAUTHORIZED', 'paid', 'login'],
])('keeps checking visible for a full second before a fast %s/%s result', async (code, status, phase) => {
  vi.stubGlobal('fetch', async () => response(code, status));
  const { result } = renderHook(() => usePaymentReturn());
  await act(async () => {});
  expect(result.current.phase).toBe('checking');
  await advance(999);
  expect(result.current.phase).toBe('checking');
  await advance(1);
  expect(result.current.phase).toBe(phase);
  if (phase === 'confirmed') {
    expect(result.current.order?.id).toBe(order.id);
    await advance(999);
    expect(result.current.phase).toBe('confirmed');
    await advance(1);
    expect(result.current.phase).toBe('filing');
  }
});

it('counts request time toward the minimum instead of delaying a slow success another second', async () => {
  let resolve: ((value: Response) => void) | undefined;
  vi.stubGlobal(
    'fetch',
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  const { result } = renderHook(() => usePaymentReturn());
  await advance(1500);
  expect(result.current.phase).toBe('checking');
  await act(async () => {
    resolve?.(response());
  });
  expect(result.current.phase).toBe('confirmed');
});

it('does not restart the visible checking time when a slow HTTP 401 clears the session', async () => {
  let resolve: ((value: Response) => void) | undefined;
  vi.stubGlobal(
    'fetch',
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  const { result, rerender } = renderHook(() => usePaymentReturn());
  const clearSession = () => {
    app.profile = null;
    rerender();
  };
  window.addEventListener('session-invalid', clearSession);
  try {
    await advance(1500);
    await act(async () => {
      resolve?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
    });
    expect(result.current.phase).toBe('login');
    expect(result.current.order).toBeUndefined();
  } finally {
    window.removeEventListener('session-invalid', clearSession);
  }
});

it('applies the same minimum to a manual retry without keeping an old failure', async () => {
  let recovered = false;
  vi.stubGlobal('fetch', async () => response(recovered ? 'OK' : 'ORDER_NOT_FOUND'));
  const { result } = renderHook(() => usePaymentReturn());
  await advance(1000);
  expect(result.current.phase).toBe('error');
  recovered = true;
  act(() => result.current.retry());
  await advance(999);
  expect(result.current.phase).toBe('checking');
  expect(result.current.error).toBeUndefined();
  await advance(1);
  expect(result.current.phase).toBe('confirmed');
});

it('cancels a delayed result when the visitor chooses to check later', async () => {
  vi.stubGlobal('fetch', async () => response());
  const { result } = renderHook(() => usePaymentReturn());
  await advance(500);
  act(() => result.current.leave());
  await advance(3000);
  expect(result.current.phase).toBe('idle');
  expect(result.current.locked).toBe(false);
  expect(location.search).toBe('');
});

it('discards a scheduled success if the signed-in account changes', async () => {
  vi.stubGlobal('fetch', async () => response());
  const { result, rerender } = renderHook(() => usePaymentReturn());
  await advance(500);
  app.profile = { id: 'different-user' };
  rerender();
  await advance(499);
  expect(result.current.phase).toBe('checking');
  await advance(1);
  expect(result.current.phase).toBe('error');
  expect(result.current.error).toMatchObject({ code: 'FORBIDDEN' });
});
