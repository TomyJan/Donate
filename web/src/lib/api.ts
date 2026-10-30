export class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
    public requestID = '',
    public requestPath = '',
  ) {
    super(message);
  }
}
let csrf = '';
export function setCSRF(v: string) {
  csrf = v;
}
function signalAccessFailure(path: string, status: number, code: string) {
  if ((status === 401 || code === 'ACCOUNT_BANNED') && path !== 'site/info' && path !== 'user/profile')
    window.dispatchEvent(new CustomEvent('session-invalid', { detail: code }));
  else if (status === 403 && code === 'FORBIDDEN' && path.startsWith('admin/'))
    window.dispatchEvent(new Event('admin-forbidden'));
}
function responseError(path: string, response: Response) {
  const code =
    response.status === 401
      ? 'UNAUTHORIZED'
      : response.status === 403
        ? 'FORBIDDEN'
        : response.status === 429
          ? 'RATE_LIMITED'
          : response.status >= 500
            ? 'SERVER_UNAVAILABLE'
            : 'INVALID_RESPONSE';
  signalAccessFailure(path, response.status, code);
  return new APIError(code, '', response.status, response.headers.get('X-Request-ID') ?? '', path);
}
async function request<T>(
  path: string,
  body: unknown,
  read: (response: Response) => Promise<T>,
  signal?: AbortSignal,
) {
  try {
    const timeout = AbortSignal.timeout(15000);
    const response = await fetch(`/api/v1/${path}`, {
      method: body === undefined ? 'GET' : 'POST',
      credentials: 'same-origin',
      headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    });
    return await read(response);
  } catch (error) {
    if (signal?.aborted) throw error;
    if (error instanceof APIError) throw error;
    const code = !navigator.onLine
      ? 'OFFLINE'
      : error instanceof DOMException && (error.name === 'TimeoutError' || error.name === 'AbortError')
        ? 'REQUEST_TIMEOUT'
        : 'NETWORK_ERROR';
    throw new APIError(code, '', 0, '', path);
  }
}
async function envelope<T>(path: string, response: Response): Promise<T> {
  let value: unknown;
  try {
    value = await response.json();
  } catch (error) {
    if (error instanceof SyntaxError) throw responseError(path, response);
    throw error;
  }
  if (!value || typeof value !== 'object' || !('code' in value) || typeof value.code !== 'string')
    throw responseError(path, response);
  const wire = value as { code: string; message?: string; request_id?: string; data: T };
  if (!response.ok || wire.code !== 'OK') {
    signalAccessFailure(path, response.status, wire.code);
    throw new APIError(
      wire.code,
      typeof wire.message === 'string' ? wire.message : '',
      response.status,
      typeof wire.request_id === 'string' ? wire.request_id : '',
      path,
    );
  }
  if (!Object.hasOwn(wire, 'data')) throw responseError(path, response);
  return wire.data;
}
export async function api<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  return request(path, body, (response) => envelope<T>(path, response), signal);
}
export async function downloadExport(id: string) {
  const path = 'admin/order/export';
  const blob = await request(path, { operation_id: id }, async (response) => {
    if (!response.ok || !response.headers.get('Content-Type')?.includes('text/csv')) {
      await envelope(path, response);
      throw responseError(path, response);
    }
    return response.blob();
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'donate-orders.csv';
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function money(fen: number) {
  return (fen / 100).toFixed(2);
}
export function dateTime(v?: string | null) {
  if (!v) return '—';
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
    .format(new Date(v))
    .replaceAll('/', '-');
}
export function query(values: Record<string, string | number | undefined>) {
  const q = new URLSearchParams();
  for (const [key, value] of Object.entries(values))
    if (value !== undefined && value !== '') q.set(key, String(value));
  return q.toString();
}
