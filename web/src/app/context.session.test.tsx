import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { LEGAL_CONSENT_KEY, LEGAL_VERSION } from '../lib/legal';
import type { Profile, Site } from '../lib/types';
import { AppProvider, useApp } from './context';

function Account() {
  const { profile } = useApp();
  return <p>{profile?.nickname ?? 'guest'}</p>;
}
function SignIn() {
  const { profile, login } = useApp();
  return (
    <>
      <p>{profile?.nickname ?? 'guest'}</p>
      <button type="button" onClick={() => void login('oidc')}>
        Sign in
      </button>
      <button type="button" onClick={() => void login('qq', true)}>
        Bind QQ
      </button>
    </>
  );
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
  localStorage.removeItem(LEGAL_CONSENT_KEY);
  history.replaceState(null, '', '/');
});

it.each([
  [false, false],
  [true, false],
  [false, true],
])('can sign in again after expiration (site delayed=%s, initial request=%s)', async (delayed, initial) => {
  const expired: Profile = {
    id: 'expired-user',
    nickname: 'Expired user',
    csrf_token: 'expired-session-csrf',
    expires_at: new Date(Date.now() - 1000).toISOString(),
    avatar_url: '',
    provider: 'oidc',
    admin: false,
    bindings: [],
  };
  const site: Site = {
    csrf_token: expired.csrf_token,
    logo: '',
    name: 'Synthetic site',
    payment_enabled: true,
    providers: ['oidc'],
    version: { version: '0.0.4', tag: '', commit: 'synthetic', built_at: '' },
  };
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  client.setQueryData(['profile'], expired);
  if (!initial) client.setQueryData(['site'], site);
  localStorage.setItem(LEGAL_CONSENT_KEY, LEGAL_VERSION);
  const loginHeaders: string[] = [];
  let finishSite: ((response: Response) => void) | undefined;
  let finishInitial: ((response: Response) => void) | undefined;
  const anonymous = { ...site, csrf_token: 'anonymous-csrf' };
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    if (path.endsWith('user/profile'))
      return Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 });
    if (path.endsWith('site/info')) {
      if (initial && !finishInitial)
        return new Promise<Response>((resolve) => {
          finishInitial = resolve;
        });
      return delayed
        ? new Promise<Response>((resolve) => {
            finishSite = resolve;
          })
        : Response.json({ code: 'OK', data: anonymous });
    }
    if (path.endsWith('user/login')) {
      const token = new Headers(init?.headers).get('X-CSRF-Token') ?? '';
      loginHeaders.push(token);
      return token === 'anonymous-csrf'
        ? Response.json({ code: 'OK', data: { authorization_url: '#authorized' } })
        : Response.json({ code: 'CSRF_FAILED', data: null }, { status: 403 });
    }
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <SignIn />
      </AppProvider>
    </QueryClientProvider>,
  );
  await screen.findByText('guest');
  if (initial) {
    await waitFor(() => expect(finishInitial).toBeTypeOf('function'));
    await act(async () => finishInitial?.(Response.json({ code: 'OK', data: site })));
  }
  fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  if (delayed) {
    await act(async () => {});
    expect(loginHeaders).toEqual([]);
    await waitFor(() => expect(finishSite).toBeTypeOf('function'));
    await act(async () => finishSite?.(Response.json({ code: 'OK', data: anonymous })));
  }
  await waitFor(() => expect(location.hash).toBe('#authorized'));
  expect(loginHeaders).toEqual(['anonymous-csrf']);
});

it('keeps a replacement session when an earlier anonymous sign-in is waiting for site data', async () => {
  const expired: Profile = {
    id: 'expired-user',
    nickname: 'Expired user',
    csrf_token: 'expired-csrf',
    expires_at: new Date(Date.now() - 1000).toISOString(),
    avatar_url: '',
    provider: 'oidc',
    admin: false,
    bindings: [],
  };
  const replacement: Profile = {
    ...expired,
    id: 'member',
    nickname: 'Member',
    csrf_token: 'member-csrf',
    expires_at: new Date(Date.now() + 30 * 86400000).toISOString(),
  };
  const site: Site = {
    csrf_token: 'expired-csrf',
    logo: '',
    name: 'Synthetic site',
    payment_enabled: true,
    providers: ['oidc', 'qq'],
    version: { version: '0.0.4', tag: '', commit: 'synthetic', built_at: '' },
  };
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  client.setQueryData(['profile'], expired);
  client.setQueryData(['site'], site);
  localStorage.setItem(LEGAL_CONSENT_KEY, LEGAL_VERSION);
  let serverProfile: Profile | null = null;
  const pendingSites: ((response: Response) => void)[] = [];
  const logins: string[] = [];
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    if (path.endsWith('user/profile'))
      return serverProfile
        ? Response.json({ code: 'OK', data: serverProfile })
        : Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 });
    if (path.endsWith('site/info'))
      return new Promise<Response>((resolve) => {
        pendingSites.push(resolve);
      });
    const token = new Headers(init?.headers).get('X-CSRF-Token') ?? '';
    if (path.endsWith('user/login')) {
      logins.push(token);
      return Response.json({ code: 'CSRF_FAILED', data: null }, { status: 403 });
    }
    if (path.endsWith('user/identity/bind'))
      return token === 'member-csrf'
        ? Response.json({ code: 'OK', data: { authorization_url: '#bound' } })
        : Response.json({ code: 'CSRF_FAILED', data: null }, { status: 403 });
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <SignIn />
      </AppProvider>
    </QueryClientProvider>,
  );
  await screen.findByText('guest');
  fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  await waitFor(() => expect(pendingSites.length).toBeGreaterThan(0));
  serverProfile = replacement;
  vi.setSystemTime(Date.now() + 16000);
  fireEvent(window, new Event('offline'));
  fireEvent(window, new Event('online'));
  await screen.findByText('Member');
  await act(async () => {
    for (const finish of pendingSites)
      finish(Response.json({ code: 'OK', data: { ...site, csrf_token: 'anonymous-csrf' } }));
  });
  expect(logins).toEqual([]);
  expect(client.getQueryData(['profile'])).toEqual(replacement);
  fireEvent.click(screen.getByRole('button', { name: 'Bind QQ' }));
  await waitFor(() => expect(location.hash).toBe('#bound'));
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
