import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LEGAL_CONSENT_KEY, LEGAL_VERSION } from '../lib/legal';
import type { Site } from '../lib/types';
import { AppProvider, useApp } from './context';

const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  vi.unstubAllGlobals();
  localStorage.clear();
  sessionStorage.clear();
  history.replaceState(null, '', '/');
});
function Controls() {
  const { login } = useApp();
  return (
    <button type="button" onClick={() => void login('campus id')}>
      Sign in
    </button>
  );
}
it.each([
  { path: '/', target: '/' },
  { path: '/admin?section=users', target: '/admin' },
  {
    path: '/?payment=returned&order=01234567-89ab-cdef-0123-456789abcdef',
    target: '/?payment=returned&order=01234567-89ab-cdef-0123-456789abcdef',
  },
  {
    path: '/auth/complete?error=authorization',
    target: '/?payment=returned&order=01234567-89ab-cdef-0123-456789abcdef',
  },
])(
  'navigates the browser directly to GET login and preserves $target from $path',
  async ({ path, target }) => {
    history.replaceState(null, '', path);
    localStorage.setItem(LEGAL_CONSENT_KEY, LEGAL_VERSION);
    const originalLocation = location;
    const assign = vi.fn();
    vi.stubGlobal('location', {
      pathname: originalLocation.pathname,
      search: originalLocation.search,
      assign,
    });
    const site: Site = {
      csrf_token: 'guest-csrf',
      logo: '',
      name: 'Site',
      payment_enabled: true,
      providers: ['campus id'],
      version: { version: '1', commit: 'test', tag: '', built_at: '' },
    };
    const requests: string[] = [];
    vi.stubGlobal('fetch', async (path: string) => {
      requests.push(path);
      if (path.endsWith('site/info')) return Response.json({ code: 'OK', data: site });
      if (path.endsWith('user/profile'))
        return Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 });
      if (path.endsWith('user/login/status'))
        return Response.json({
          code: 'OK',
          data: { return_to: '/?payment=returned&order=01234567-89ab-cdef-0123-456789abcdef' },
        });
      if (path.endsWith('user/login'))
        return Response.json({ code: 'OK', data: { authorization_url: '#legacy-fetch' } });
      throw new Error(`Unexpected request: ${path}`);
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    clients.push(client);
    client.setQueryData(['profile'], null);
    client.setQueryData(['site'], site);
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <Controls />
        </AppProvider>
      </QueryClientProvider>,
    );
    await act(async () => {});
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(assign).toHaveBeenCalledOnce());
    expect(assign).toHaveBeenCalledWith(
      `/api/v1/user/login?${new URLSearchParams({ provider: 'campus id', return_to: target })}`,
    );
    expect(requests).not.toContain('/api/v1/user/login');
  },
);
