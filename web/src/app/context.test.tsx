import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from './context';

function Account() {
  const app = useApp();
  return (
    <>
      <span>{app.profile?.nickname ?? 'guest'}</span>
      <button type="button" onClick={() => void app.logout()}>
        Log out
      </button>
    </>
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
it('immediately clears the displayed identity and private cache when logging out from the menu', async () => {
  let loggedIn = true;
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url.endsWith('user/logout')) loggedIn = false;
      return Promise.resolve(
        new Response(
          JSON.stringify({
            code: 'OK',
            data: url.endsWith('user/profile')
              ? loggedIn
                ? { id: 'user', nickname: 'Alice', csrf_token: 'session-csrf' }
                : null
              : url.endsWith('site/info')
                ? { csrf_token: 'guest-csrf' }
                : null,
          }),
        ),
      );
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['order-detail', true, 'private-order'], { message: 'private-message' });
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <Account />
      </AppProvider>
    </QueryClientProvider>,
  );
  await screen.findByText('Alice');
  await waitFor(() => expect(client.isFetching()).toBe(0));
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  fireEvent.click(screen.getByRole('button', { name: 'Log out' }));
  await screen.findByText('guest');
  expect(client.getQueryData(['order-detail', true, 'private-order'])).toBeUndefined();
  client.clear();
});
it('renews using session CSRF even while site information is still loading', async () => {
  let header = '';
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (url.endsWith('site/info')) return new Promise(() => {});
      if (url.endsWith('user/session/renew')) header = new Headers(init?.headers).get('X-CSRF-Token') ?? '';
      return Promise.resolve(
        new Response(
          JSON.stringify({
            code: 'OK',
            data: url.endsWith('user/profile') ? { id: 'user', csrf_token: 'session-csrf' } : null,
          }),
        ),
      );
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <div />
      </AppProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(header).toBe('session-csrf'));
  client.clear();
});

function ObservedAccount() {
  const { profile, logout } = useApp();
  return profile ? (
    <button type="button" onClick={() => void logout()}>
      Sign out
    </button>
  ) : (
    <p>Signed out</p>
  );
}

it('immediately clears the observed account and protected data after a successful logout', async () => {
  let signedOut = false;
  const ok = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }));
  vi.stubGlobal('fetch', async (path: string) => {
    if (path.endsWith('/user/logout')) {
      signedOut = true;
      return ok(null);
    }
    if (path.endsWith('/site/info')) return ok({ csrf_token: 'guest-csrf', providers: ['oidc'] });
    if (path.endsWith('/user/profile'))
      return signedOut
        ? new Response(JSON.stringify({ code: 'UNAUTHORIZED', data: null }), { status: 401 })
        : ok({ id: 'administrator', nickname: 'Admin', admin: true, csrf_token: 'csrf', bindings: [] });
    if (path.endsWith('/user/session/renew')) return ok(null);
    throw new Error(`Unexpected request: ${path}`);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 15000 } } });
  client.setQueryData(['admin-user', 'private-user'], { nickname: 'Private user' });
  client.setQueryData(['overview', '30d'], { paid_fen: 123 });
  try {
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <ObservedAccount />
        </AppProvider>
      </QueryClientProvider>,
    );
    const button = await screen.findByRole('button', { name: 'Sign out' });
    await waitFor(() => expect(client.isFetching()).toBe(0));
    fireEvent.click(button);
    await screen.findByText('Signed out');
    expect(signedOut).toBe(true);
    expect(client.getQueryData(['profile'])).toBeNull();
    expect(client.getQueryData(['admin-user', 'private-user'])).toBeUndefined();
    expect(client.getQueryData(['overview', '30d'])).toBeUndefined();
  } finally {
    client.clear();
  }
});
