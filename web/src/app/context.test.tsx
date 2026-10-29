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
