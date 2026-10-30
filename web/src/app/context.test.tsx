import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { Profile } from '../lib/types';
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
  vi.useRealTimers();
  vi.restoreAllMocks();
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
  let renewed = false;
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (url.endsWith('site/info')) return new Promise(() => {});
      if (url.endsWith('user/session/renew')) {
        header = new Headers(init?.headers).get('X-CSRF-Token') ?? '';
        renewed = true;
      }
      return Promise.resolve(
        new Response(
          JSON.stringify({
            code: 'OK',
            data: url.endsWith('user/profile')
              ? {
                  id: 'user',
                  csrf_token: 'session-csrf',
                  expires_at: new Date(Date.now() + (renewed ? 30 : 20) * 86400000).toISOString(),
                }
              : null,
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

const day = 86400000;
const renewalNow = new Date('2027-01-01T00:00:00Z');
async function flushSession() {
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    await vi.advanceTimersByTimeAsync(0);
  });
}
function renewalFixture(remaining: number) {
  vi.useFakeTimers();
  vi.setSystemTime(renewalNow);
  let expiry = new Date(Date.now() + remaining).toISOString();
  let valid = true;
  const requests: string[] = [];
  const identity = () => ({
    id: 'window-user',
    nickname: 'Alice',
    admin: false,
    csrf_token: 'window-csrf',
    bindings: [],
    expires_at: expiry,
  });
  vi.stubGlobal('fetch', async (url: string) => {
    requests.push(url);
    const data = url.endsWith('user/profile')
      ? identity()
      : url.endsWith('site/info')
        ? { csrf_token: 'guest-csrf' }
        : null;
    if (url.endsWith('user/session/renew')) expiry = new Date(Date.now() + 30 * day).toISOString();
    return new Response(JSON.stringify({ code: valid ? 'OK' : 'UNAUTHORIZED', data: valid ? data : null }), {
      status: valid ? 200 : 401,
    });
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: Infinity } },
  });
  client.setQueryData(['profile'], identity());
  client.setQueryData(['site'], { csrf_token: 'guest-csrf' });
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <Account />
      </AppProvider>
    </QueryClientProvider>,
  );
  return {
    client,
    requests,
    renewals: () => requests.filter((path) => path.endsWith('user/session/renew')).length,
    invalidate: () => {
      valid = false;
    },
    setServerExpiry: (value: string) => {
      expiry = value;
    },
    mountSecond: () => {
      const second = new QueryClient({
        defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: Infinity } },
      });
      second.setQueryData(['profile'], identity());
      second.setQueryData(['site'], { csrf_token: 'guest-csrf' });
      render(
        <QueryClientProvider client={second}>
          <AppProvider>
            <div />
          </AppProvider>
        </QueryClientProvider>,
      );
      return second;
    },
  };
}

it('does not request renewal outside the window and renews at the exact threshold', async () => {
  const fixture = renewalFixture(20 * day + 1000);
  await flushSession();
  fireEvent.pointerDown(window);
  fireEvent.focus(window);
  await flushSession();
  expect(fixture.renewals()).toBe(0);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  expect(fixture.client.getQueryData<{ expires_at: string }>(['profile'])?.expires_at).toBe(
    '2027-01-31T00:00:01.000Z',
  );
  fireEvent.pointerDown(window);
  fireEvent.focus(window);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10 * 60000);
  });
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  fixture.client.clear();
});

it('waits for fresh visible activity after entering the window while idle', async () => {
  const fixture = renewalFixture(20 * day + 6 * 60000);
  await flushSession();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(6 * 60000);
  });
  await flushSession();
  expect(fixture.renewals()).toBe(0);
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  fireEvent.keyDown(window);
  await flushSession();
  expect(fixture.renewals()).toBe(0);
  hidden.mockReturnValue(false);
  fireEvent(document, new Event('visibilitychange'));
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  fixture.client.clear();
});

it('coalesces focus and interaction renewal and synchronizes the fresh expiry', async () => {
  const fixture = renewalFixture(20 * day);
  fireEvent.focus(window);
  fireEvent.pointerDown(window);
  fireEvent.keyDown(window);
  fireEvent(document, new Event('visibilitychange'));
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  expect(fixture.client.getQueryData<{ expires_at: string }>(['profile'])?.expires_at).toBe(
    '2027-01-31T00:00:00.000Z',
  );
  fixture.client.clear();
});

it('recovers an expired cached session through profile validation without renewing it', async () => {
  const fixture = renewalFixture(1000);
  // Hide before the initial renewal microtask; an idle tab must not extend its session.
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  fixture.client.setQueryData(['order-detail', true, 'private'], { secret: 'private' });
  await flushSession();
  fixture.invalidate();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  hidden.mockReturnValue(false);
  fireEvent.focus(window);
  await flushSession();
  expect(fixture.renewals()).toBe(0);
  expect(fixture.client.getQueryData(['profile'])).toBeNull();
  expect(fixture.client.getQueryData(['order-detail', true, 'private'])).toBeUndefined();
  fixture.client.clear();
});

it('shares a pending renewal across simultaneous session observers', async () => {
  const fixture = renewalFixture(20 * day);
  const second = fixture.mountSecond();
  fireEvent.focus(window);
  fireEvent.pointerDown(window);
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  expect(second.getQueryData<{ expires_at: string }>(['profile'])?.expires_at).toBe(
    '2027-01-31T00:00:00.000Z',
  );
  fixture.client.clear();
  second.clear();
});

it('does not keep an idle tab logged in through background renewal timers', async () => {
  const fixture = renewalFixture(20 * day);
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10 * day);
  });
  await flushSession();
  expect(fixture.renewals()).toBe(1);
  fireEvent.keyDown(window);
  await flushSession();
  expect(fixture.renewals()).toBe(2);
  expect(fixture.client.getQueryData<{ expires_at: string }>(['profile'])?.expires_at).toBe(
    '2027-02-10T00:00:00.000Z',
  );
  fixture.client.clear();
});

it('uses a renewal received from another tab before the boundary timer fires', async () => {
  const fixture = renewalFixture(20 * day + 1000);
  await flushSession();
  fireEvent(
    window,
    new StorageEvent('storage', {
      key: 'donate-session-renewed',
      newValue: JSON.stringify({
        id: 'window-user',
        previous_expires_at: '2027-01-21T00:00:01.000Z',
        expires_at: '2027-01-31T00:00:00.000Z',
      }),
    }),
  );
  await flushSession();
  fireEvent.focus(window);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  await flushSession();
  expect(fixture.renewals()).toBe(0);
  expect(fixture.client.getQueryData<{ expires_at: string }>(['profile'])?.expires_at).toBe(
    '2027-01-31T00:00:00.000Z',
  );
  fixture.client.clear();
});

it('checks the server expiry under the tab lock before sending renewal', async () => {
  let updateExpiry = (_value: string) => {};
  vi.stubGlobal('navigator', {
    onLine: true,
    locks: {
      request: async (_name: string, action: () => Promise<unknown>) => {
        // A different tab finishes renewing while this tab waits for its lock.
        updateExpiry('2027-01-31T00:00:00.000Z');
        return action();
      },
    },
  });
  const fixture = renewalFixture(20 * day);
  updateExpiry = fixture.setServerExpiry;
  await flushSession();
  expect(fixture.renewals()).toBe(0);
  expect(fixture.client.getQueryData<{ expires_at: string }>(['profile'])?.expires_at).toBe(
    '2027-01-31T00:00:00.000Z',
  );
  fixture.client.clear();
});

it.each(['replacement-user', 'window-user'])(
  'does not renew old credentials when the session changes to %s while waiting for the tab lock',
  async (replacementID) => {
    let replaceSession = () => {};
    vi.stubGlobal('navigator', {
      onLine: true,
      locks: {
        request: async (_name: string, action: () => Promise<unknown>) => {
          replaceSession();
          return action();
        },
      },
    });
    const fixture = renewalFixture(20 * day);
    replaceSession = () =>
      fixture.client.setQueryData(['profile'], {
        id: replacementID,
        nickname: 'Bob',
        csrf_token: 'replacement-csrf',
        expires_at: '2027-01-31T00:00:00.000Z',
      });
    await flushSession();
    expect(fixture.renewals()).toBe(0);
    expect(fixture.client.getQueryData<{ csrf_token: string }>(['profile'])?.csrf_token).toBe(
      'replacement-csrf',
    );
    expect(screen.getByText('Bob')).toBeInTheDocument();
    fixture.client.clear();
  },
);

it.each(
  [1, 2].flatMap((pendingRead) =>
    ['replacement-user', 'window-user'].flatMap((replacementID) =>
      ['OK', 'UNAUTHORIZED'].map((responseCode) => ({ pendingRead, replacementID, responseCode })),
    ),
  ),
)(
  'ignores old $responseCode profile read $pendingRead after replacement with $replacementID',
  async ({ pendingRead, replacementID, responseCode }) => {
    const fixture = renewalFixture(20 * day);
    const old = fixture.client.getQueryData<Profile>(['profile']) as Profile;
    let finish: ((response: Response) => void) | undefined;
    let reads = 0;
    let renewCount = 0;
    vi.stubGlobal('fetch', async (url: string) => {
      if (url.endsWith('user/profile')) {
        reads++;
        if (reads === pendingRead)
          return new Promise<Response>((resolve) => {
            finish = resolve;
          });
        return Response.json({ code: 'OK', data: old });
      }
      if (url.endsWith('user/session/renew')) renewCount++;
      return Response.json({ code: 'OK', data: null });
    });
    await flushSession();
    expect(finish).toBeTypeOf('function');
    const replacement = {
      ...old,
      id: replacementID,
      nickname: 'Bob',
      csrf_token: 'replacement-csrf',
      expires_at: '2027-01-31T00:00:00.000Z',
    };
    act(() => fixture.client.setQueryData(['profile'], replacement));
    await flushSession();
    await act(async () => {
      finish?.(
        Response.json(
          { code: responseCode, data: responseCode === 'OK' ? old : null },
          { status: responseCode === 'OK' ? 200 : 401 },
        ),
      );
    });
    await flushSession();
    expect(fixture.client.getQueryData(['profile'])).toEqual(replacement);
    expect(screen.getByText('Bob')).toBeInTheDocument();
    expect(renewCount).toBe(pendingRead - 1);
    fixture.client.clear();
  },
);

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
