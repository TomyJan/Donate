import { afterEach, expect, it, vi } from 'vitest';
import { APIError, api, downloadExport, setCSRF } from './api';

it.each(['user/login/status', 'user/login/qq/complete'])(
  'does not revoke the current user when a flow targets a banned account: %s',
  async (path) => {
    const invalid = vi.fn();
    window.addEventListener('session-invalid', invalid);
    try {
      setCSRF('valid-current-B');
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(Response.json({ code: 'ACCOUNT_BANNED', data: null }, { status: 403 })),
      );
      await expect(
        api(path, path.endsWith('complete') ? { qq_number: '12345' } : undefined),
      ).rejects.toMatchObject({ code: 'ACCOUNT_BANNED', status: 403 });
      expect(invalid).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('session-invalid', invalid);
    }
  },
);

afterEach(() => {
  setCSRF('');
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it.each(
  ['user/login/status', 'user/login/qq/complete'].flatMap((path) =>
    ['json', 'text'].map((format) => ({ path, format })),
  ),
)('does not revoke a session for a flow-only 401: $path/$format', async ({ path, format }) => {
  const expired = vi.fn();
  window.addEventListener('session-invalid', expired);
  try {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          format === 'json'
            ? Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 })
            : new Response('expired flow', { status: 401 }),
        ),
    );
    await expect(
      api(path, path.endsWith('complete') ? { qq_number: '12345' } : undefined),
    ).rejects.toMatchObject({ status: 401 });
    expect(expired).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener('session-invalid', expired);
  }
});

it.each([
  [401, 'UNAUTHORIZED'],
  [403, 'ACCOUNT_BANNED'],
  [200, 'ACCOUNT_BANNED'],
] as const)('still revokes the active session for a protected API %s/%s', async (status, code) => {
  const expired = vi.fn();
  window.addEventListener('session-invalid', expired);
  try {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ code, data: null }, { status })));
    await expect(api('order/detail?id=current')).rejects.toMatchObject({ code });
    expect(expired).toHaveBeenCalledOnce();
  } finally {
    window.removeEventListener('session-invalid', expired);
  }
});

it.each([
  ['json 401', 401, 'UNAUTHORIZED'],
  ['json 403', 403, 'FORBIDDEN'],
  ['plain 401', 401, ''],
  ['business ban', 200, 'ACCOUNT_BANNED'],
] as const)('does not signal access failure for replaced credentials: %s', async (_label, status, code) => {
  let finish: ((response: Response) => void) | undefined;
  vi.stubGlobal(
    'fetch',
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  const failures: string[] = [];
  const observe = (event: Event) => failures.push(event.type);
  window.addEventListener('session-invalid', observe);
  window.addEventListener('admin-forbidden', observe);
  try {
    setCSRF('previous-session');
    const pending = api('admin/overview');
    setCSRF('replacement-session');
    finish?.(code ? Response.json({ code, data: null }, { status }) : new Response('expired', { status }));
    await expect(pending).rejects.toMatchObject({ status });
    expect(failures).toEqual([]);
  } finally {
    window.removeEventListener('session-invalid', observe);
    window.removeEventListener('admin-forbidden', observe);
  }
});

it('does not revive old request credentials after switching away and back', async () => {
  let finish: ((response: Response) => void) | undefined;
  vi.stubGlobal(
    'fetch',
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  const expired = vi.fn();
  window.addEventListener('session-invalid', expired);
  try {
    setCSRF('original-session');
    const pending = api('order/detail?id=old');
    setCSRF('other-session');
    setCSRF('original-session');
    finish?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
    await expect(pending).rejects.toMatchObject({ status: 401 });
    expect(expired).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener('session-invalid', expired);
  }
});

it.each(['denied', 'csv'] as const)(
  'ignores an export after credentials change without a caller guard: %s',
  async (kind) => {
    let finish: ((response: Response) => void) | undefined;
    vi.stubGlobal(
      'fetch',
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    );
    const denied = vi.fn();
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    window.addEventListener('admin-forbidden', denied);
    try {
      setCSRF('previous-session');
      const pending = downloadExport('previous-export');
      setCSRF('replacement-session');
      finish?.(
        kind === 'denied'
          ? Response.json({ code: 'FORBIDDEN', data: null }, { status: 403 })
          : new Response('id\nold', { headers: { 'Content-Type': 'text/csv' } }),
      );
      if (kind === 'denied') await expect(pending).rejects.toMatchObject({ status: 403 });
      else await pending;
      expect(denied).not.toHaveBeenCalled();
      expect(click).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('admin-forbidden', denied);
    }
  },
);
it.each(['denied', 'csv'] as const)('ignores a stale export %s response', async (kind) => {
  let current = true;
  let finish: ((value: Response) => void) | undefined;
  vi.stubGlobal(
    'fetch',
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  const forbidden = vi.fn();
  window.addEventListener('admin-forbidden', forbidden);
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
  try {
    const pending = downloadExport('old-export', () => current);
    current = false;
    finish?.(
      kind === 'denied'
        ? Response.json({ code: 'FORBIDDEN', data: null }, { status: 403 })
        : new Response('id,amount\nold,100', { headers: { 'Content-Type': 'text/csv' } }),
    );
    if (kind === 'denied') await expect(pending).rejects.toMatchObject({ code: 'FORBIDDEN' });
    else await pending;
    expect(forbidden).not.toHaveBeenCalled();
    expect(click).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener('admin-forbidden', forbidden);
  }
});
it.each(['json', 'text'])('ignores a canceled request denial received as %s', async (format) => {
  const controller = new AbortController();
  let finish: ((response: Response) => void) | undefined;
  vi.stubGlobal(
    'fetch',
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  let expired = false;
  const onExpired = () => {
    expired = true;
  };
  window.addEventListener('session-invalid', onExpired);
  try {
    const pending = api('order/detail?id=one', undefined, controller.signal);
    controller.abort();
    finish?.(
      format === 'json'
        ? Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 })
        : new Response('expired', { status: 401 }),
    );
    await expect(pending).rejects.toMatchObject({ status: 401 });
    expect(expired).toBe(false);
  } finally {
    window.removeEventListener('session-invalid', onExpired);
  }
});

function streamResponse(contentType: string) {
  vi.stubGlobal(
    'fetch',
    vi.fn((_url: string, options: RequestInit) =>
      Promise.resolve(
        new Response(
          new ReadableStream({
            start(stream) {
              options.signal?.addEventListener('abort', () => stream.error(options.signal?.reason), {
                once: true,
              });
            },
          }),
          { headers: { 'Content-Type': contentType } },
        ),
      ),
    ),
  );
}
it.each(['json', 'csv'])('classifies a timeout while reading the %s response body', async (kind) => {
  const timeout = new AbortController();
  vi.spyOn(AbortSignal, 'timeout').mockReturnValue(timeout.signal);
  streamResponse(kind === 'json' ? 'application/json' : 'text/csv');
  const pending = kind === 'json' ? api('order/list') : downloadExport('export-id');
  await Promise.resolve();
  timeout.abort(new DOMException('timed out', 'TimeoutError'));
  await expect(pending).rejects.toMatchObject({
    code: 'REQUEST_TIMEOUT',
    status: 0,
    requestPath: kind === 'json' ? 'order/list' : 'admin/order/export',
  });
});
it('preserves caller cancellation while reading a response body', async () => {
  streamResponse('application/json');
  const caller = new AbortController();
  const pending = api('order/list', undefined, caller.signal);
  await Promise.resolve();
  caller.abort();
  await expect(pending).rejects.toBe(caller.signal.reason);
});
it('classifies a connection lost during response body reading as a network failure', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(
      new Response(
        new ReadableStream({
          start(stream) {
            stream.error(new TypeError('connection closed'));
          },
        }),
      ),
    ),
  );
  await expect(api('order/list')).rejects.toMatchObject({ code: 'NETWORK_ERROR', status: 0 });
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
