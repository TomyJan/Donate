import {
  onlineManager,
  QueryClient,
  QueryClientProvider,
  useQueries,
  useQueryClient,
} from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Home } from '../features/Home';
import { api } from '../lib/api';
import { sessionIdentity } from '../lib/session-scope';
import type { Profile, Site } from '../lib/types';
import { AppProvider, useApp } from './context';

const ok = (data: unknown) => Response.json({ code: 'OK', data });
const initial = Date.parse('2026-11-01T00:00:00Z');
const alice: Profile = {
  id: 'alice',
  nickname: 'Alice',
  csrf_token: 'alice-csrf',
  expires_at: '2099-01-01T00:00:00Z',
  bindings: [],
  avatar_url: '',
  provider: 'oidc',
  admin: false,
};
const site: Site = {
  csrf_token: 'guest',
  logo: '',
  name: 'test',
  payment_enabled: true,
  providers: [],
  version: { version: '1', tag: '', commit: 'test', built_at: '' },
};
let client: QueryClient;
function Who() {
  const { profile } = useApp();
  return <p data-testid="profile">{profile?.nickname ?? 'guest'}</p>;
}
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(initial);
  onlineManager.setOnline(true);
  localStorage.clear();
  sessionStorage.clear();
  localStorage.setItem('donate-language', 'en');
  history.replaceState(null, '', '/');
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('scrollTo', vi.fn());
});
afterEach(() => {
  cleanup();
  client?.clear();
  onlineManager.setOnline(true);
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it.each(['different user', 'same user new CSRF'])(
  'does not reuse another session private records after automatic reconnect: %s',
  async (kind) => {
    const bob = {
      ...alice,
      id: kind === 'different user' ? 'bob' : alice.id,
      nickname: 'Bob',
      csrf_token: 'bob-csrf',
    };
    let owner = alice;
    let phase = 0;
    let releaseBobProfile: ((response: Response) => void) | undefined;
    let releaseAliceRecords: ((response: Response) => void) | undefined;
    const calls: string[] = [];
    const record = (person: Profile) => ({
      id: `${person.csrf_token}-order`,
      is_mine: true,
      nickname: person.nickname,
      avatar_url: '',
      amount_fen: 888,
      status: 'pending',
      message: `${person.nickname.toUpperCase()} PRIVATE RECORD`,
      province: '',
      date: '2026-11-01T00:00:00Z',
    });
    vi.stubGlobal('fetch', (input: string, init?: RequestInit) => {
      const path = String(input);
      calls.push(`${phase}:${path}`);
      if (path.endsWith('site/info')) return Promise.resolve(ok(site));
      if (path.endsWith('user/profile')) {
        if (phase === 1)
          return new Promise<Response>((resolve, reject) => {
            releaseBobProfile = resolve;
            init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true });
          });
        return Promise.resolve(ok(owner));
      }
      if (path.includes('/order/list?')) {
        if (phase === 2 && path.includes('only_mine=true'))
          return new Promise<Response>((resolve, reject) => {
            releaseAliceRecords = resolve;
            init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true });
          });
        return Promise.resolve(
          ok({ items: path.includes('only_mine=true') ? [record(owner)] : [], next_cursor: '' }),
        );
      }
      throw new Error(path);
    });
    client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <div className="d-app">
            <Who />
            <Home />
          </div>
        </AppProvider>
      </QueryClientProvider>,
    );
    await screen.findByText('Alice');
    fireEvent.click(screen.getByRole('button', { name: /^Only mine$/ }));
    await screen.findByText('ALICE PRIVATE RECORD');
    owner = bob;
    phase = 1;
    vi.setSystemTime(initial + 16000);
    fireEvent(window, new Event('offline'));
    fireEvent(window, new Event('online'));
    await waitFor(() => expect(releaseBobProfile).toBeTypeOf('function'));
    await waitFor(() =>
      expect(calls.some((path) => path.startsWith('1:') && path.includes('only_mine=true'))).toBe(true),
    );
    await act(async () => releaseBobProfile?.(ok(bob)));
    await waitFor(() => expect(screen.getByTestId('profile')).toHaveTextContent('Bob'));
    await waitFor(() => expect(client.isFetching()).toBe(0));
    owner = alice;
    phase = 2;
    vi.setSystemTime(initial + 32000);
    fireEvent(window, new Event('offline'));
    fireEvent(window, new Event('online'));
    await waitFor(() => expect(screen.getByTestId('profile')).toHaveTextContent('Alice'));
    await waitFor(() => expect(releaseAliceRecords).toBeTypeOf('function'));
    expect(screen.queryByText('BOB PRIVATE RECORD')).not.toBeInTheDocument();
    const records = client.getQueriesData({ queryKey: ['records'] });
    expect(JSON.stringify(records)).not.toContain('BOB PRIVATE RECORD');
    await act(async () => releaseAliceRecords?.(ok({ items: [record(alice)], next_cursor: '' })));
  },
);

const privateRoots = [
  'records',
  'order-detail',
  'admin-list',
  'operation',
  'overview',
  'admin-user',
  'admin-user-options',
  'admin-tasks',
];
function PrivateQueries() {
  const { profile } = useApp();
  const client = useQueryClient();
  const scope = sessionIdentity(profile);
  useQueries({
    queries: privateRoots.map((root) => ({
      queryKey: [root, scope],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        api(
          `order/list?fixture=${root}`,
          undefined,
          signal,
          () => sessionIdentity(client.getQueryData<Profile | null>(['profile'])) === scope,
        ),
      enabled: !!profile,
      retry: false,
    })),
  });
  return <Who />;
}

it.each(['different user', 'same user new CSRF', 'admin revoked', 'guest'])(
  'cancels and deletes every private query when automatic profile scope changes: %s',
  async (kind) => {
    const first = { ...alice, admin: true };
    const replacement =
      kind === 'guest'
        ? null
        : {
            ...first,
            id: kind === 'different user' ? 'bob' : first.id,
            nickname: 'Replacement',
            csrf_token: kind === 'admin revoked' ? first.csrf_token : 'new-csrf',
            admin: kind !== 'admin revoked',
          };
    let owner: Profile | null = first;
    let hold = false;
    let releaseProfile: ((response: Response) => void) | undefined;
    const pending: { signal: AbortSignal; resolve: (response: Response) => void }[] = [];
    vi.stubGlobal('fetch', (path: string, init?: RequestInit) => {
      if (path.endsWith('site/info')) return Promise.resolve(ok(site));
      if (path.endsWith('user/profile')) {
        if (hold)
          return new Promise<Response>((resolve, reject) => {
            releaseProfile = resolve;
            init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true });
          });
        return Promise.resolve(
          owner ? ok(owner) : Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }),
        );
      }
      if (path.includes('order/list?fixture=')) {
        if (!hold) return Promise.resolve(ok({ secret: 'initial-private-cache' }));
        return new Promise<Response>((resolve, reject) => {
          const signal = init?.signal as AbortSignal;
          pending.push({ signal, resolve });
          signal.addEventListener('abort', () => reject(signal.reason), { once: true });
        });
      }
      throw new Error(path);
    });
    client = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <PrivateQueries />
        </AppProvider>
      </QueryClientProvider>,
    );
    await waitFor(() =>
      expect(privateRoots.every((root) => client.getQueryData([root, sessionIdentity(first)]))).toBe(true),
    );
    hold = true;
    owner = replacement;
    vi.setSystemTime(initial + 16000);
    fireEvent(window, new Event('offline'));
    fireEvent(window, new Event('online'));
    await waitFor(() => expect(pending).toHaveLength(privateRoots.length));
    const old = [...pending];
    await act(async () =>
      releaseProfile?.(
        owner ? ok(owner) : Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }),
      ),
    );
    await waitFor(() =>
      expect(screen.getByTestId('profile')).toHaveTextContent(owner ? 'Replacement' : 'guest'),
    );
    expect(old.every(({ signal }) => signal.aborted)).toBe(true);
    for (const root of privateRoots)
      expect(client.getQueryData([root, sessionIdentity(first)])).toBeUndefined();
    await act(async () => {
      for (const request of old) request.resolve(ok({ secret: 'late-old-session' }));
    });
    expect(JSON.stringify(client.getQueriesData({}))).not.toContain('late-old-session');
    expect(client.getQueryData(['profile'])).toEqual(replacement);
  },
);

it('revalidates a mismatched real read while preserving the observed profile until discovery completes', async () => {
  const replacement = { ...alice, id: 'bob', nickname: 'Bob', csrf_token: 'bob-csrf' };
  let owner = alice;
  let releaseProfile: ((response: Response) => void) | undefined;
  let reads = 0;
  vi.stubGlobal('fetch', (path: string, init?: RequestInit) => {
    if (path.endsWith('site/info')) return Promise.resolve(ok(site));
    if (path.endsWith('user/profile')) {
      if (owner === replacement)
        return new Promise<Response>((resolve, reject) => {
          releaseProfile = resolve;
          init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true });
        });
      return Promise.resolve(ok(owner));
    }
    if (path.includes('order/list?fixture=')) {
      reads += 1;
      const expected = new Headers(init?.headers).get('X-Session-Scope');
      return Promise.resolve(
        expected === owner.csrf_token
          ? ok({ secret: owner.nickname })
          : Response.json(
              { code: 'CSRF_FAILED', data: null },
              { status: 403, headers: { 'X-Session-Mismatch': '1' } },
            ),
      );
    }
    throw new Error(path);
  });
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <PrivateQueries />
      </AppProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(reads).toBe(privateRoots.length));
  await waitFor(() => expect(client.isFetching()).toBe(0));
  owner = replacement;
  await act(async () => {
    await client.refetchQueries({ queryKey: ['records'] });
  });
  await waitFor(() => expect(releaseProfile).toBeTypeOf('function'));
  expect(screen.getByTestId('profile')).toHaveTextContent('Alice');
  expect(JSON.stringify(client.getQueriesData({}))).not.toContain('Bob');
  await act(async () => releaseProfile?.(ok(replacement)));
  await waitFor(() => expect(screen.getByTestId('profile')).toHaveTextContent('Bob'));
  expect(client.getQueryData(['profile'])).toEqual(replacement);
  expect(client.getQueryData(['records', sessionIdentity(alice)])).toBeUndefined();
});
