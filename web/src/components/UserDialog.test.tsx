import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import { UserDialog } from './UserDialog';

const state = vi.hoisted(() => ({
  report: vi.fn(),
  notify: vi.fn(),
  profile: {
    id: 'administrator',
    admin: true,
    nickname: 'Administrator',
    avatar_url: '',
    provider: 'oidc',
    csrf_token: 'csrf',
    expires_at: '2099-01-01T00:00:00Z',
    bindings: [],
  },
}));
vi.mock('../app/context', () => ({
  useApp: () => ({
    t: (key: string) => translate(key, 'en'),
    profile: state.profile,
    report: state.report,
    notify: state.notify,
  }),
}));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  state.report.mockClear();
  state.notify.mockClear();
});
it('shows account information and opens its orders without embedding or fetching donation history', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  const requests: string[] = [];
  vi.stubGlobal('fetch', async (input: string) => {
    requests.push(input);
    if (input.includes('/admin/order/list'))
      return new Response(
        JSON.stringify({ code: 'OK', data: { items: [], total: 0, page: 1, page_size: 10 } }),
        { status: 200 },
      );
    return new Response(
      JSON.stringify({
        code: 'OK',
        data: {
          id: 'target-user',
          nickname: 'Donor',
          avatar_url: '',
          status: 'active',
          admin: false,
          bindings: [],
          created_at: '2026-10-29T00:00:00Z',
          last_login_at: '2026-10-30T00:00:00Z',
          order_count: 3,
          paid_fen: 100,
        },
      }),
      { status: 200 },
    );
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['profile'], state.profile);
  const onViewOrders = vi.fn();
  const onClose = vi.fn();
  try {
    render(
      <QueryClientProvider client={client}>
        <div className="d-app">
          <UserDialog id="target-user" onClose={onClose} onViewOrders={onViewOrders} />
        </div>
      </QueryClientProvider>,
    );
    await screen.findByText('Donor');
    expect(screen.getByText('Donation amount').parentElement).toHaveTextContent('¥1.00');
    expect(document.querySelector('.user-history')).toBeNull();
    expect(requests.some((path) => path.includes('/admin/order/list'))).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: translate('viewDonations', 'en') }));
    expect(onViewOrders).toHaveBeenCalledWith('target-user');
    expect(onClose).toHaveBeenCalledOnce();
  } finally {
    client.clear();
  }
});

const bindings = [
  {
    id: 'qq-binding',
    provider: 'qq',
    nickname: 'QQ donor',
    avatar_url: '',
    bound_at: '2026-10-29T00:00:00Z',
  },
  {
    id: 'oidc-binding',
    provider: 'oidc',
    nickname: 'OIDC donor',
    avatar_url: '',
    bound_at: '2026-10-30T00:00:00Z',
  },
];
const target = {
  id: 'target-user',
  nickname: 'Donor',
  avatar_url: '',
  status: 'active',
  admin: false,
  bindings,
  created_at: '2026-10-29T00:00:00Z',
  order_count: 3,
  paid_fen: 988,
};
const envelope = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 });
function mountTarget(client: QueryClient) {
  client.setQueryData(['profile'], state.profile);
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  return render(
    <QueryClientProvider client={client}>
      <div className="d-app">
        <UserDialog id="target-user" onClose={() => {}} onViewOrders={() => {}} />
      </div>
    </QueryClientProvider>,
  );
}
async function confirmUnlink() {
  fireEvent.click(await screen.findByRole('button', { name: 'Unlink QQ' }));
  const popup = await screen.findByRole('dialog', { name: 'Unlink QQ' });
  await waitFor(() => expect(within(popup).getByRole('button', { name: 'Confirm' })).toBeEnabled());
  fireEvent.click(within(popup).getByRole('button', { name: 'Confirm' }));
}

it('only posts the selected identity after confirmation and displays server refreshed bindings', async () => {
  const requests: { path: string; body?: string | null }[] = [];
  let unlinked = false;
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    requests.push({ path, body: init?.body as string | undefined });
    if (path === '/api/v1/admin/user/identity/unbind') {
      unlinked = true;
      return envelope(null);
    }
    if (path.startsWith('/api/v1/admin/user/detail?'))
      return envelope({ ...target, bindings: unlinked ? bindings.slice(1) : bindings });
    throw new Error(`Unexpected request: ${path}`);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const keys = [
    ['admin-list', 'users'],
    ['admin-list', 'audit'],
    ['admin-list', 'orders'],
    ['admin-user-options', ''],
    ['records', 'guest', false],
  ];
  for (const key of keys) client.setQueryData(key, { cached: true });
  try {
    mountTarget(client);
    fireEvent.click(await screen.findByRole('button', { name: 'Unlink QQ' }));
    expect(requests.filter((request) => request.body)).toEqual([]);
    const popup = await screen.findByRole('dialog', { name: 'Unlink QQ' });
    await waitFor(() => expect(within(popup).getByRole('button', { name: 'Confirm' })).toBeEnabled());
    fireEvent.click(within(popup).getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(screen.queryByText('QQ donor')).not.toBeInTheDocument());
    expect(screen.getByText('OIDC donor')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Unlink OIDC' })).toBeDisabled();
    expect(requests.filter((request) => request.body)).toEqual([
      { path: '/api/v1/admin/user/identity/unbind', body: '{"id":"target-user","identity_id":"qq-binding"}' },
    ]);
    expect(requests.filter((request) => request.path.includes('/detail?'))).toHaveLength(2);
    for (const key of keys) expect(client.getQueryState(key)?.isInvalidated).toBe(true);
  } finally {
    client.clear();
  }
});

it('keeps the identity displayed and reports a last-identity server rejection', async () => {
  vi.stubGlobal('fetch', async (path: string) => {
    if (path === '/api/v1/admin/user/identity/unbind')
      return new Response(
        JSON.stringify({
          code: 'LAST_IDENTITY_FORBIDDEN',
          message: 'Keep at least one sign-in method.',
          data: null,
        }),
        { status: 200 },
      );
    if (path.startsWith('/api/v1/admin/user/detail?')) return envelope(target);
    throw new Error(`Unexpected request: ${path}`);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  try {
    mountTarget(client);
    await confirmUnlink();
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Action failed'));
    expect(screen.getByText('QQ donor')).toBeInTheDocument();
    expect(state.report.mock.calls[0]?.[0]).toMatchObject({ code: 'LAST_IDENTITY_FORBIDDEN' });
    expect(state.notify).not.toHaveBeenCalled();
  } finally {
    client.clear();
  }
});
