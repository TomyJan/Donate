import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Profile, User } from '../lib/types';
import { UserDialog } from './UserDialog';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});

function AdminHost() {
  const { profile: current, loading } = useApp();
  return current?.admin ? (
    <UserDialog id="administrator" onClose={() => {}} onViewOrders={() => {}} />
  ) : (
    <p>{loading ? 'Loading account' : 'Signed out'}</p>
  );
}

it('refreshes the real profile first and leaves admin UI when the removed identity held the current session', async () => {
  localStorage.setItem('donate-language', 'en');
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  const bindings = [
    {
      id: 'qq-binding',
      provider: 'qq',
      nickname: 'QQ administrator',
      avatar_url: '',
      bound_at: '2026-10-29T00:00:00Z',
    },
    {
      id: 'oidc-binding',
      provider: 'oidc',
      nickname: 'OIDC administrator',
      avatar_url: '',
      bound_at: '2026-10-30T00:00:00Z',
    },
  ];
  const profile: Profile = {
    id: 'administrator',
    admin: true,
    nickname: 'Administrator',
    avatar_url: '',
    bindings,
    csrf_token: 'csrf',
    expires_at: '2026-10-31T00:00:00Z',
    provider: 'qq',
  };
  const user: User = {
    ...profile,
    status: 'active',
    order_count: 0,
    paid_fen: 0,
    created_at: '2026-10-29T00:00:00Z',
  };
  const requests: string[] = [];
  let revoked = false;
  const ok = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 });
  vi.stubGlobal('fetch', async (path: string) => {
    requests.push(path);
    if (path === '/api/v1/site/info') return ok({ csrf_token: 'guest-csrf', providers: ['qq', 'oidc'] });
    if (path === '/api/v1/user/profile')
      return revoked
        ? new Response(JSON.stringify({ code: 'UNAUTHORIZED', message: 'Sign in required', data: null }), {
            status: 401,
          })
        : ok(profile);
    if (path === '/api/v1/user/session/renew') return ok(null);
    if (path === '/api/v1/admin/user/identity/unbind') {
      revoked = true;
      return ok(null);
    }
    if (path.startsWith('/api/v1/admin/user/detail?')) return ok(user);
    throw new Error(`Unexpected request: ${path}`);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  try {
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <div className="d-app">
            <AdminHost />
          </div>
        </AppProvider>
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Unlink QQ' }));
    const popup = await screen.findByRole('dialog', { name: 'Unlink QQ' });
    await waitFor(() => expect(within(popup).getByRole('button', { name: 'Confirm' })).toBeEnabled());
    fireEvent.click(within(popup).getByRole('button', { name: 'Confirm' }));
    await screen.findByText('Signed out');
    expect(client.getQueryData(['profile'])).toBeNull();
    expect(screen.queryByRole('dialog', { name: 'User details' })).not.toBeInTheDocument();
    const afterUnlink = requests.slice(requests.indexOf('/api/v1/admin/user/identity/unbind') + 1);
    expect(afterUnlink[0]).toBe('/api/v1/user/profile');
    expect(afterUnlink.some((path) => path.startsWith('/api/v1/admin/'))).toBe(false);
  } finally {
    client.clear();
  }
});
