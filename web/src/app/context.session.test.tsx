import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { Profile } from '../lib/types';
import { AppProvider, useApp } from './context';

function Account() {
  const { profile } = useApp();
  return <p>{profile?.nickname ?? 'guest'}</p>;
}
const clients: QueryClient[] = [];
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-31T00:00:00Z'));
  onlineManager.setOnline(true);
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  onlineManager.setOnline(true);
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const denials = [
  { status: 401, code: 'UNAUTHORIZED', json: true },
  { status: 403, code: 'ACCOUNT_BANNED', json: true },
  { status: 200, code: 'ACCOUNT_BANNED', json: true },
  { status: 401, code: 'UNAUTHORIZED', json: false },
];
it.each(denials)(
  'revokes the current session on renewal $status/$code (JSON=$json)',
  async ({ status, code, json }) => {
    const profile: Profile = {
      id: 'current-user',
      nickname: 'Current user',
      csrf_token: 'current-csrf',
      expires_at: new Date(Date.now() + 20 * 86400000).toISOString(),
      avatar_url: '',
      provider: 'qq',
      admin: false,
      bindings: [],
    };
    const ok = (data: unknown) => Response.json({ code: 'OK', data });
    vi.stubGlobal('fetch', async (path: string) => {
      if (path.endsWith('user/profile')) return ok(profile);
      if (path.endsWith('site/info')) return ok({ csrf_token: 'guest-csrf' });
      if (path.endsWith('user/session/renew'))
        return json ? Response.json({ code, data: null }, { status }) : new Response('expired', { status });
      throw new Error('Unexpected synthetic request');
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    clients.push(client);
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <Account />
        </AppProvider>
      </QueryClientProvider>,
    );
    await waitFor(() => expect(client.getQueryData(['profile'])).toBeNull());
    expect(screen.getByText('guest')).toBeInTheDocument();
  },
);

it.each(denials.flatMap((denial) => ['bob', 'alice'].map((id) => ({ ...denial, id }))))(
  'ignores late renewal $status/$code (JSON=$json) after a new session for $id',
  async ({ status, code, json, id }) => {
    const now = Date.now();
    const alice: Profile = {
      id: 'alice',
      nickname: 'Alice',
      csrf_token: 'alice-csrf',
      expires_at: new Date(now + 20 * 86400000 + 30000).toISOString(),
      avatar_url: '',
      provider: 'oidc',
      admin: false,
      bindings: [],
    };
    const replacement: Profile = {
      ...alice,
      id,
      nickname: 'Replacement session',
      csrf_token: 'replacement-csrf',
      expires_at: new Date(now + 30 * 86400000).toISOString(),
    };
    let serverProfile = alice;
    let finishRenew: ((response: Response) => void) | undefined;
    const ok = (data: unknown) => Response.json({ code: 'OK', data });
    vi.stubGlobal('fetch', async (path: string) => {
      if (path.endsWith('user/profile')) return ok(serverProfile);
      if (path.endsWith('site/info')) return ok({ csrf_token: 'guest-csrf' });
      if (path.endsWith('user/session/renew'))
        return new Promise<Response>((resolve) => {
          finishRenew = resolve;
        });
      throw new Error('Unexpected synthetic request');
    });
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: 1, staleTime: 15000, refetchOnWindowFocus: false },
        mutations: { retry: false },
      },
    });
    clients.push(client);
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <Account />
        </AppProvider>
      </QueryClientProvider>,
    );
    await screen.findByText('Alice');
    vi.setSystemTime(now + 31000);
    fireEvent.pointerDown(window);
    await waitFor(() => expect(finishRenew).toBeTypeOf('function'));
    // A second tab changes the cookie. A reconnect obtains the replacement via the real profile query.
    serverProfile = replacement;
    fireEvent(window, new Event('offline'));
    fireEvent(window, new Event('online'));
    await screen.findByText('Replacement session');
    await act(async () => {
      finishRenew?.(
        json ? Response.json({ code, data: null }, { status }) : new Response('expired', { status }),
      );
    });
    expect(client.getQueryData(['profile'])).toEqual(replacement);
    expect(screen.getByText('Replacement session')).toBeInTheDocument();
  },
);
