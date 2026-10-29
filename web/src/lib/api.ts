export class APIError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
    public requestID = '',
  ) {
    super(message);
  }
}
let csrf = '';
export function setCSRF(v: string) {
  csrf = v;
}
export async function api<T>(path: string, body?: unknown): Promise<T> {
  const r = await fetch(`/api/v1/${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let v: { code: string; message: string; data: T; request_id: string };
  try {
    v = await r.json();
  } catch {
    throw new APIError('NETWORK_ERROR', '服务暂不可用', r.status);
  }
  if (!r.ok || v.code !== 'OK') {
    if ((r.status === 401 || v.code === 'ACCOUNT_BANNED') && path !== 'site/info' && path !== 'user/profile')
      window.dispatchEvent(new CustomEvent('session-invalid', { detail: v.code }));
    throw new APIError(v.code, v.message, r.status, v.request_id);
  }
  return v.data;
}
export async function downloadExport(id: string) {
  const r = await fetch('/api/v1/admin/order/export', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify({ operation_id: id }),
  });
  if (!r.ok || !r.headers.get('Content-Type')?.includes('text/csv')) {
    const v = await r.json();
    throw new APIError(v.code, v.message, r.status, v.request_id);
  }
  const url = URL.createObjectURL(await r.blob());
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
