import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { BrowserRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import { setCSRF } from '../lib/api';
import type { Profile } from '../lib/types';
import { Admin } from './Admin';

const a: Profile = {
  id: 'admin-a',
  nickname: 'Admin A',
  admin: true,
  avatar_url: '',
  provider: 'oidc',
  csrf_token: 'csrf-a',
  expires_at: '2099-01-01T00:00:00Z',
  bindings: [],
};
const b: Profile = { ...a, id: 'admin-b', nickname: 'Admin B', csrf_token: 'csrf-b' };
const donor = {
  id: 'donor',
  nickname: 'Donor',
  avatar_url: '',
  status: 'active',
  admin: false,
  created_at: '2026-10-01T00:00:00Z',
  last_login_at: '2026-10-01T00:00:00Z',
  order_count: 1,
  paid_fen: 100,
  bindings: [
    { id: 'qq', provider: 'qq', nickname: 'QQ donor', avatar_url: '', bound_at: '2026-10-01T00:00:00Z' },
    {
      id: 'oidc',
      provider: 'oidc',
      nickname: 'OIDC donor',
      avatar_url: '',
      bound_at: '2026-10-01T00:00:00Z',
    },
  ],
};
const order = {
  id: 'order-one',
  user_id: 'donor',
  nickname: 'Donor',
  avatar_url: '',
  amount_fen: 100,
  channel: 'alipay',
  status: 'paid',
  message: 'Message',
  merchant_order_no: 'DONATE-1',
  created_at: '2026-10-01T00:00:00Z',
  expires_at: '2099-01-01T00:00:00Z',
  hidden_order: false,
  hidden_message: false,
};
const operation = {
  id: 'export-op',
  action: 'export',
  confirmed: false,
  expires_at: '2099-01-01T00:00:00Z',
  items: [{ id: order.id, status: 'ready', result: 'ready' }],
};
const ok = (data: unknown) => Response.json({ code: 'OK', data });
let client: QueryClient,
  serverProfile: Profile,
  heldPath: string,
  finish: ((value: Response) => void) | undefined,
  requests: string[];
function Observe() {
  const { profile, toasts } = useApp();
  return (
    <>
      <p data-testid="identity">
        {profile?.id ?? 'guest'}:{profile?.csrf_token ?? ''}
      </p>
      {toasts.map((t) => (
        <p key={t.id} data-testid="toast">
          {t.text}
        </p>
      ))}
    </>
  );
}
beforeEach(() => {
  localStorage.clear();
  localStorage.setItem('donate-language', 'en');
  serverProfile = a;
  heldPath = '';
  finish = undefined;
  requests = [];
  Element.prototype.scrollIntoView = vi.fn();
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = new URL(String(input), 'http://localhost').pathname;
    requests.push(path);
    if (path === heldPath)
      return new Promise<Response>((resolve) => {
        finish = resolve;
      });
    if (path.endsWith('/user/profile')) return ok(serverProfile);
    if (path.endsWith('/site/info'))
      return ok({
        csrf_token: 'guest',
        name: 'Donate',
        providers: ['qq', 'oidc'],
        payment_enabled: true,
        version: {},
      });
    if (path.endsWith('/admin/overview'))
      return ok({ paid_fen: 0, paid: 0, pending: 0, closed: 0, daily: [], channels: [] });
    if (path.endsWith('/admin/user/list')) return ok({ items: [donor], total: 1, page: 1, page_size: 20 });
    if (path.endsWith('/admin/user/detail')) return ok(donor);
    if (path.endsWith('/admin/order/list')) return ok({ items: [order], total: 1, page: 1, page_size: 20 });
    if (path.endsWith('/admin/order/detail')) return ok(order);
    if (path.endsWith('/admin/order/action/preview')) return ok(operation);
    if (path.endsWith('/admin/order/action/execute') || path.endsWith('/admin/operation/detail'))
      return ok({
        ...operation,
        confirmed: true,
        items: [{ id: order.id, status: 'done', result: 'exported' }],
      });
    if (path.endsWith('/admin/order/export'))
      return new Response('id\norder-one', { headers: { 'Content-Type': 'text/csv' } });
    throw new Error(`Unexpected request ${path}`);
  });
  client = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } });
  mountAdmin();
});
function mountAdmin() {
  return render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <BrowserRouter>
          <div className="d-app">
            <Admin />
            <Observe />
          </div>
        </BrowserRouter>
      </AppProvider>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  client.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
});
async function replaceFromOtherTab(next = b) {
  serverProfile = next;
  fireEvent(window, new Event('focus'));
  await waitFor(() => expect(client.getQueryData<Profile>(['profile'])?.csrf_token).toBe(next.csrf_token));
  expect(screen.getByTestId('identity')).toHaveTextContent(`${next.id}:${next.csrf_token}`);
}
async function openOrders() {
  const navigation = await screen.findByRole('navigation');
  fireEvent.click(within(navigation).getByRole('button', { name: 'Orders' }));
  await screen.findByText('Donor');
  fireEvent.click(screen.getAllByRole('checkbox')[1]);
}
async function openUserAction(action: 'Ban' | 'Unlink QQ') {
  const navigation = await screen.findByRole('navigation');
  fireEvent.click(within(navigation).getByRole('button', { name: 'Users' }));
  fireEvent.click(await screen.findByRole('button', { name: 'User details donor' }));
  const dialog = await screen.findByRole('dialog', { name: 'User details' });
  fireEvent.click(await within(dialog).findByRole('button', { name: action }));
  const confirmation = await screen.findByRole('dialog', { name: action });
  await waitFor(() => expect(within(confirmation).getByRole('button', { name: 'Confirm' })).toBeEnabled());
  fireEvent.click(within(confirmation).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
}
async function confirmExport() {
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  const dialog = await screen.findByRole('dialog', { name: 'Export' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
}
it('preserves the replacement session when an old bulk preview returns 401', async () => {
  heldPath = '/api/v1/admin/order/action/preview';
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  await replaceFromOtherTab();
  await act(async () => {
    finish?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
  });
  expect(client.getQueryData<Profile>(['profile'])?.id).toBe('admin-b');
});
it.each([
  ['Ban', 'ban'],
  ['Unlink QQ', 'identity/unbind'],
] as const)('preserves the replacement session when an old %s returns 401', async (label, path) => {
  heldPath = `/api/v1/admin/user/${path}`;
  await openUserAction(label);
  await replaceFromOtherTab();
  await act(async () => {
    finish?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
  });
  expect(client.getQueryData<Profile>(['profile'])?.id).toBe('admin-b');
});
it('does not request an old export using replacement credentials', async () => {
  heldPath = '/api/v1/admin/order/action/execute';
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  const dialog = await screen.findByRole('dialog', { name: 'Export' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  await replaceFromOtherTab();
  const count = requests.filter((p) => p.endsWith('/admin/order/export')).length;
  await act(async () => {
    finish?.(ok({ ...operation, confirmed: true }));
  });
  expect(requests.filter((p) => p.endsWith('/admin/order/export'))).toHaveLength(count);
});
it.each([
  ['preview', '/api/v1/admin/order/action/preview'],
  ['Ban', '/api/v1/admin/user/ban'],
  ['Unlink QQ', '/api/v1/admin/user/identity/unbind'],
] as const)('does not revoke replacement permissions when an old %s returns 403', async (label, path) => {
  heldPath = path;
  if (label === 'preview') {
    await openOrders();
    fireEvent.click(screen.getByRole('button', { name: 'Export' }));
    await waitFor(() => expect(finish).toBeTypeOf('function'));
  } else await openUserAction(label);
  await replaceFromOtherTab();
  const revoked = vi.fn();
  window.addEventListener('admin-forbidden', revoked);
  try {
    await act(async () => {
      finish?.(Response.json({ code: 'FORBIDDEN', data: null }, { status: 403 }));
    });
    expect(revoked).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener('admin-forbidden', revoked);
  }
});
it.each([
  ['Ban', 'ban'],
  ['Unlink QQ', 'identity/unbind'],
] as const)('does not display a successful old %s in the replacement session', async (label, path) => {
  heldPath = `/api/v1/admin/user/${path}`;
  await openUserAction(label);
  await replaceFromOtherTab();
  expect(screen.queryByText('Done')).not.toBeInTheDocument();
  await act(async () => {
    finish?.(ok(null));
  });
  expect(screen.queryByText('Done')).not.toBeInTheDocument();
});

it('invalidates a pending preview when the same user gets a new session token', async () => {
  heldPath = '/api/v1/admin/order/action/preview';
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  await replaceFromOtherTab({ ...a, csrf_token: 'csrf-replaced' });
  await act(async () => {
    finish?.(ok(operation));
  });
  expect(screen.queryByRole('dialog', { name: 'Export' })).not.toBeInTheDocument();
  expect(client.getQueryData<Profile>(['profile'])?.csrf_token).toBe('csrf-replaced');
});

it('does not revive a pending preview after switching away and back to the same credentials', async () => {
  heldPath = '/api/v1/admin/order/action/preview';
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  await replaceFromOtherTab();
  await replaceFromOtherTab(a);
  await act(async () => {
    finish?.(ok(operation));
  });
  expect(screen.queryByRole('dialog', { name: 'Export' })).not.toBeInTheDocument();
});

it('still clears the current session when its own preview returns 401', async () => {
  heldPath = '/api/v1/admin/order/action/preview';
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  await act(async () => {
    finish?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
  });
  expect(client.getQueryData(['profile'])).toBeNull();
});

it.each([
  ['Ban', 'ban'],
  ['Unlink QQ', 'identity/unbind'],
] as const)('still completes %s for the current session', async (label, path) => {
  heldPath = `/api/v1/admin/user/${path}`;
  await openUserAction(label);
  await act(async () => {
    finish?.(ok(null));
  });
  expect(screen.getByText('Done')).toBeInTheDocument();
});

it.each([
  ['execute', 'UNAUTHORIZED', 401],
  ['execute', 'FORBIDDEN', 403],
  ['download', 'UNAUTHORIZED', 401],
  ['download', 'FORBIDDEN', 403],
] as const)('ignores an old export %s returning %s', async (stage, code, status) => {
  heldPath = stage === 'execute' ? '/api/v1/admin/order/action/execute' : '/api/v1/admin/order/export';
  await confirmExport();
  await replaceFromOtherTab();
  const accessFailures: string[] = [];
  const observe = (event: Event) => accessFailures.push(event.type);
  window.addEventListener('admin-forbidden', observe);
  window.addEventListener('session-invalid', observe);
  try {
    await act(async () => {
      finish?.(Response.json({ code, data: null }, { status }));
    });
    expect(client.getQueryData<Profile>(['profile'])?.id).toBe('admin-b');
    expect(accessFailures).toEqual([]);
    expect(HTMLAnchorElement.prototype.click).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener('admin-forbidden', observe);
    window.removeEventListener('session-invalid', observe);
  }
});

it('does not download a late CSV into a replacement session', async () => {
  heldPath = '/api/v1/admin/order/export';
  await confirmExport();
  await replaceFromOtherTab();
  await act(async () => {
    finish?.(new Response('id\norder-one', { headers: { 'Content-Type': 'text/csv' } }));
  });
  expect(HTMLAnchorElement.prototype.click).not.toHaveBeenCalled();
  expect(screen.queryByText('Done')).not.toBeInTheDocument();
});

it('still downloads a confirmed export for the current session', async () => {
  heldPath = '/api/v1/admin/order/export';
  await confirmExport();
  await act(async () => {
    finish?.(new Response('id\norder-one', { headers: { 'Content-Type': 'text/csv' } }));
  });
  expect(HTMLAnchorElement.prototype.click).toHaveBeenCalledOnce();
});

it('ignores a successful user action after the administration component is remounted', async () => {
  heldPath = '/api/v1/admin/user/ban';
  await openUserAction('Ban');
  cleanup();
  mountAdmin();
  await waitFor(() => expect(screen.getByTestId('identity')).toHaveTextContent('admin-a:csrf-a'));
  await act(async () => {
    finish?.(ok(null));
  });
  expect(screen.queryByText('Done')).not.toBeInTheDocument();
  expect(client.getQueryData<Profile>(['profile'])?.id).toBe('admin-a');
});

it('does not let a superseded preview clear the session or unlock the newer preview', async () => {
  heldPath = '/api/v1/admin/order/action/preview';
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  const finishOld = finish;
  fireEvent.click(within(screen.getByRole('navigation')).getByRole('button', { name: 'Users' }));
  await screen.findByText('Donor');
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await waitFor(() => expect(finish).not.toBe(finishOld));
  await act(async () => {
    finishOld?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
  });
  expect(client.getQueryData<Profile>(['profile'])?.id).toBe('admin-a');
  expect(screen.getByRole('button', { name: 'Export' })).toBeDisabled();
  await act(async () => {
    finish?.(ok(operation));
  });
  expect(await screen.findByRole('dialog', { name: 'Export' })).toBeInTheDocument();
});

it.each([
  ['Ban', 'ban'],
  ['Unlink QQ', 'identity/unbind'],
] as const)('ignores an old %s error when credentials change before React renders', async (label, path) => {
  heldPath = `/api/v1/admin/user/${path}`;
  await openUserAction(label);
  const finishOld = finish;
  let released = false;
  const unsubscribe = client.getQueryCache().subscribe(() => {
    if (!released && client.getQueryData<Profile>(['profile'])?.csrf_token === b.csrf_token) {
      released = true;
      finishOld?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
    }
  });
  try {
    await replaceFromOtherTab();
    expect(released).toBe(true);
    expect(screen.queryAllByTestId('toast')).toHaveLength(0);
  } finally {
    unsubscribe();
  }
});

it.each([false, true])(
  'handles an initial current overview denial with cached profile=%s',
  async (cached) => {
    cleanup();
    client.clear();
    setCSRF('');
    heldPath = '/api/v1/admin/overview';
    finish = undefined;
    if (cached) client.setQueryData(['profile'], a);
    mountAdmin();
    await waitFor(() => expect(finish).toBeTypeOf('function'));
    await waitFor(() => expect(client.getQueryData<Profile>(['profile'])?.id).toBe('admin-a'));
    await act(async () => {
      finish?.(Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }));
    });
    expect(client.getQueryData(['profile'])).toBeNull();
  },
);

it.each([
  ['preview', 403],
  ['execute', 200],
  ['execute', 401],
  ['execute', 403],
] as const)(
  'revokes order-dialog %s HTTP %s before a replacement profile renders',
  async (endpoint, status) => {
    heldPath = `/api/v1/admin/order/action/${endpoint}`;
    await openOrders();
    fireEvent.click(screen.getByRole('button', { name: 'Order details order-on' }));
    const detail = await screen.findByRole('dialog', { name: 'Order details' });
    fireEvent.click(await within(detail).findByRole('button', { name: 'Export' }));
    if (endpoint === 'execute') {
      const confirmation = await screen.findByRole('dialog', { name: 'Export' });
      await waitFor(() =>
        expect(within(confirmation).getByRole('button', { name: 'Confirm' })).toBeEnabled(),
      );
      fireEvent.click(within(confirmation).getByRole('button', { name: 'Confirm' }));
    }
    await waitFor(() => expect(finish).toBeTypeOf('function'));
    let released = false;
    const unsubscribe = client.getQueryCache().subscribe(() => {
      if (!released && client.getQueryData<Profile>(['profile'])?.csrf_token === b.csrf_token) {
        released = true;
        finish?.(
          status === 200
            ? ok({ ...operation, confirmed: true })
            : Response.json({ code: status === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN', data: null }, { status }),
        );
      }
    });
    try {
      await replaceFromOtherTab();
      expect(released).toBe(true);
      expect(requests.filter((path) => path.endsWith('/admin/order/export'))).toHaveLength(0);
      expect(screen.queryAllByTestId('toast')).toHaveLength(0);
      expect(client.getQueryData<Profile>(['profile'])?.id).toBe(b.id);
    } finally {
      unsubscribe();
    }
  },
);

it('still exports from the order dialog for the current administrator', async () => {
  await openOrders();
  fireEvent.click(screen.getByRole('button', { name: 'Order details order-on' }));
  const detail = await screen.findByRole('dialog', { name: 'Order details' });
  fireEvent.click(await within(detail).findByRole('button', { name: 'Export' }));
  const confirmation = await screen.findByRole('dialog', { name: 'Export' });
  await waitFor(() => expect(within(confirmation).getByRole('button', { name: 'Confirm' })).toBeEnabled());
  fireEvent.click(within(confirmation).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(HTMLAnchorElement.prototype.click).toHaveBeenCalledOnce());
  expect(requests.filter((path) => path.endsWith('/admin/order/export'))).toHaveLength(1);
  expect(client.getQueryData<Profile>(['profile'])?.id).toBe(a.id);
});
