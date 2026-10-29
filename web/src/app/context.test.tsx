import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppProvider } from './context';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
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
