import { afterEach, expect, it, vi } from 'vitest';
import { APIError, api, setCSRF } from './api';

afterEach(() => vi.unstubAllGlobals());
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
