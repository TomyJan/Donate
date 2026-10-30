import { afterEach, expect, it, vi } from 'vitest';
import { APIError, api, setCSRF } from './api';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it.each([
  [502, 'SERVER_UNAVAILABLE'],
  [429, 'RATE_LIMITED'],
  [200, 'INVALID_RESPONSE'],
])('classifies a non-JSON HTTP %s response without showing a generic service error', async (status, code) => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('<html>proxy response</html>', { status })));
  await expect(api('order/list')).rejects.toMatchObject({ code, status });
});
it('recognizes an expired session even when a proxy returns a plain-text 401', async () => {
  const expired = vi.fn();
  window.addEventListener('session-invalid', expired);
  try {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('expired', { status: 401 })));
    await expect(api('order/detail?id=one')).rejects.toMatchObject({ code: 'UNAUTHORIZED', status: 401 });
    expect(expired).toHaveBeenCalledOnce();
  } finally {
    window.removeEventListener('session-invalid', expired);
  }
});
it('distinguishes an offline browser from a connection failure', async () => {
  vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
  await expect(api('order/list')).rejects.toMatchObject({ code: 'OFFLINE', status: 0 });
});
it('classifies a timed-out request and rejects invalid success envelopes', async () => {
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockRejectedValueOnce(new DOMException('timed out', 'TimeoutError'))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 'OK' }))),
  );
  await expect(api('site/info')).rejects.toMatchObject({ code: 'REQUEST_TIMEOUT' });
  await expect(api('site/info')).rejects.toMatchObject({ code: 'INVALID_RESPONSE' });
});
it('rejects HTTP-200 business errors and handles an expired session centrally', async () => {
  let expired = false;
  const handler = () => {
    expired = true;
  };
  window.addEventListener('session-invalid', handler);
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ code: 'ORDER_NOT_PAYABLE', message: 'closed', data: null, request_id: 'one' }),
          { status: 200 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ code: 'UNAUTHORIZED', message: 'login', data: null, request_id: 'two' }),
          { status: 401 },
        ),
      ),
  );
  await expect(api('order/payment/start', { id: 'one' })).rejects.toBeInstanceOf(APIError);
  await expect(api('order/detail?id=one')).rejects.toMatchObject({ code: 'UNAUTHORIZED' });
  expect(expired).toBe(true);
  window.removeEventListener('session-invalid', handler);
});
it('sends same-origin credentials and session-bound CSRF on writes', async () => {
  setCSRF('csrf');
  const fn = vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'OK', data: { id: 'order' } })));
  vi.stubGlobal('fetch', fn);
  await api('order/create', { amount_fen: 1 });
  expect(fn.mock.calls[0][1]).toMatchObject({
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': 'csrf' },
  });
});
it('signals revoked admin access only for an admin permission denial', async () => {
  const denied = vi.fn();
  window.addEventListener('admin-forbidden', denied);
  const error = (code: string) => new Response(JSON.stringify({ code, data: null }), { status: 403 });
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce(error('CSRF_FAILED'))
      .mockResolvedValueOnce(error('FORBIDDEN'))
      .mockResolvedValueOnce(error('FORBIDDEN')),
  );
  try {
    await expect(api('admin/overview')).rejects.toMatchObject({ code: 'CSRF_FAILED' });
    await expect(api('order/detail?id=other')).rejects.toMatchObject({ code: 'FORBIDDEN' });
    expect(denied).not.toHaveBeenCalled();
    await expect(api('admin/overview')).rejects.toMatchObject({ code: 'FORBIDDEN' });
    expect(denied).toHaveBeenCalledTimes(1);
  } finally {
    window.removeEventListener('admin-forbidden', denied);
  }
});
