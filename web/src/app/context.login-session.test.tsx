import { onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { LEGAL_CONSENT_KEY, LEGAL_VERSION } from '../lib/legal';
import type { Profile, Site } from '../lib/types';
import { AppProvider, useApp } from './context';

const alice: Profile = {
  id: 'alice',
  nickname: 'Alice',
  csrf_token: 'csrf-alice',
  expires_at: '2099-01-01T00:00:00Z',
  avatar_url: '',
  provider: 'oidc',
  admin: false,
  bindings: [],
};
const bob: Profile = { ...alice, id: 'bob', nickname: 'Replacement', csrf_token: 'csrf-bob' };
const site: Site = {
  csrf_token: 'guest-csrf',
  logo: '',
  name: 'Synthetic site',
  payment_enabled: true,
  providers: ['oidc'],
  version: { version: '1', commit: 'test', tag: '', built_at: '' },
};
const clients: QueryClient[] = [];
const ok = (data: unknown) => Response.json({ code: 'OK', data });
function Controls() {
  const app = useApp();
  return (
    <>
      <p>{app.profile?.nickname ?? 'guest'}</p>
      <button type="button" onClick={() => void app.login('oidc')}>
        Sign in
      </button>
      <button type="button" onClick={() => void app.logout()}>
        Log out
      </button>
      <output data-testid="toasts">{JSON.stringify(app.toasts)}</output>
    </>
  );
}
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-11-01T00:00:00Z'));
  localStorage.setItem(LEGAL_CONSENT_KEY, LEGAL_VERSION);
  onlineManager.setOnline(true);
  history.replaceState(null, '', '/');
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  onlineManager.setOnline(true);
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.clear();
  history.replaceState(null, '', '/');
});
async function fixture(initial: Profile | null, pauseBeforePost = false, logout = false) {
  let serverProfile = initial;
  let finish: (response: Response) => void = () => {
    throw new Error('request not started');
  };
  let fail: (error: Error) => void = () => {
    throw new Error('request not started');
  };
  let signal: AbortSignal | null | undefined;
  let startedAt = 0;
  let startedDate = 0;
  const requests: string[] = [];
  if (pauseBeforePost) history.replaceState(null, '', '/auth/complete');
  vi.stubGlobal('fetch', (path: string, init?: RequestInit) => {
    requests.push(path);
    if (path.endsWith(pauseBeforePost ? 'user/login/status' : logout ? 'user/logout' : 'user/login')) {
      signal = init?.signal;
      startedAt = performance.now();
      startedDate = Date.now();
      return new Promise<Response>((resolve, reject) => {
        const abort = () => reject(signal?.reason ?? new DOMException('Aborted', 'AbortError'));
        if (signal?.aborted) {
          abort();
          return;
        }
        signal?.addEventListener('abort', abort, { once: true });
        finish = (response) => {
          signal?.removeEventListener('abort', abort);
          if (signal?.aborted) abort();
          else resolve(response);
        };
        fail = (error) => {
          signal?.removeEventListener('abort', abort);
          reject(error);
        };
      });
    }
    if (path.endsWith('user/login')) return Promise.resolve(ok({ authorization_url: '#authorized' }));
    if (path.endsWith('site/info')) return Promise.resolve(ok(site));
    if (path.endsWith('user/profile'))
      return Promise.resolve(
        serverProfile
          ? ok(serverProfile)
          : Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 }),
      );
    throw new Error(`Unexpected request: ${path}`);
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  client.setQueryData(['profile'], initial);
  client.setQueryData(['site'], site);
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <Controls />
      </AppProvider>
    </QueryClientProvider>,
  );
  await screen.findByText(initial?.nickname ?? 'guest');
  await act(async () => {});
  // Only the already-loaded page ages; no request is pending during this advance.
  vi.setSystemTime(Date.now() + 16000);
  fireEvent.click(screen.getByRole('button', { name: logout ? 'Log out' : 'Sign in' }));
  await waitFor(() =>
    expect(requests).toContain(
      `/api/v1/${pauseBeforePost ? 'user/login/status' : logout ? 'user/logout' : 'user/login'}`,
    ),
  );
  return {
    client,
    requests,
    finish: (response: Response) => finish(response),
    fail: (error: Error) => fail(error),
    assertLiveRequest: () => {
      expect(signal?.aborted).toBe(false);
      expect(performance.now() - startedAt).toBeLessThan(15000);
      expect(Date.now() - startedDate).toBe(0);
    },
    reconnect: async (replacement: Profile | null) => {
      serverProfile = replacement;
      fireEvent(window, new Event('offline'));
      fireEvent(window, new Event('online'));
      await screen.findByText(replacement?.nickname ?? 'guest');
      await waitFor(() => expect(client.getQueryData(['profile'])).toEqual(replacement));
    },
  };
}

it.each(
  ['anonymous', 'different-user', 'same-id-new-csrf', 'user-aba', 'anonymous-aba'].flatMap((replacement) =>
    ['success', '401', '403', 'banned', 'network'].map((outcome) => ({ replacement, outcome })),
  ),
)(
  'ignores an old ordinary login $outcome after $replacement replacement',
  async ({ replacement, outcome }) => {
    const initial = replacement.startsWith('anonymous') ? null : alice;
    const f = await fixture(initial);
    const next =
      replacement === 'same-id-new-csrf'
        ? { ...alice, nickname: 'Replacement', csrf_token: 'new-alice-csrf' }
        : bob;
    if (replacement.endsWith('aba')) {
      act(() => {
        f.client.setQueryData(['profile'], next);
        f.client.setQueryData(['profile'], initial);
      });
    } else await f.reconnect(next);
    const expected = replacement.endsWith('aba') ? initial : next;
    f.client.setQueryData(['order-detail', false, 'current-private'], { message: 'current-private' });
    f.assertLiveRequest();
    await act(async () => {
      if (outcome === 'network') f.fail(new TypeError('connection lost'));
      else
        f.finish(
          outcome === 'success'
            ? ok({ authorization_url: '#stale-login' })
            : Response.json(
                {
                  code:
                    outcome === '401'
                      ? 'UNAUTHORIZED'
                      : outcome === 'banned'
                        ? 'ACCOUNT_BANNED'
                        : 'FORBIDDEN',
                  data: null,
                },
                { status: outcome === '401' ? 401 : 403 },
              ),
        );
    });
    expect(location.hash).toBe('');
    expect(f.client.getQueryData(['profile'])).toEqual(expected);
    expect(f.client.getQueryData(['order-detail', false, 'current-private'])).toEqual({
      message: 'current-private',
    });
    expect(screen.getByTestId('toasts')).toHaveTextContent('[]');
  },
);

it.each(['switch', 'same-id-new-csrf', 'aba'])(
  'does not send ordinary login after its preflight scope changes: %s',
  async (replacement) => {
    const f = await fixture(alice, true);
    act(() => {
      f.client.setQueryData(
        ['profile'],
        replacement === 'same-id-new-csrf' ? { ...alice, csrf_token: 'new-csrf' } : bob,
      );
      if (replacement === 'aba') f.client.setQueryData(['profile'], alice);
    });
    f.assertLiveRequest();
    await act(async () => f.finish(ok({ return_to: '/' })));
    expect(f.requests).not.toContain('/api/v1/user/login');
    expect(location.hash).toBe('');
  },
);

it.each([null, alice])('allows a current explicitly initiated normal login (%s)', async (initial) => {
  const f = await fixture(initial);
  f.assertLiveRequest();
  await act(async () => f.finish(ok({ authorization_url: '#authorized' })));
  await waitFor(() => expect(location.hash).toBe('#authorized'));
});

it.each(
  ['different-user', 'same-id-new-csrf', 'aba'].flatMap((replacement) =>
    ['success', '401', '403', 'network'].map((outcome) => ({ replacement, outcome })),
  ),
)('ignores an old logout $outcome after $replacement replacement', async ({ replacement, outcome }) => {
  const f = await fixture(alice, false, true);
  const next =
    replacement === 'same-id-new-csrf'
      ? { ...alice, nickname: 'Replacement', csrf_token: 'new-alice-csrf' }
      : bob;
  if (replacement === 'aba') {
    act(() => {
      f.client.setQueryData(['profile'], next);
      f.client.setQueryData(['profile'], alice);
    });
  } else await f.reconnect(next);
  const expected = replacement === 'aba' ? alice : next;
  f.client.setQueryData(['order-detail', false, 'current-private'], { message: 'current-private' });
  f.assertLiveRequest();
  await act(async () => {
    if (outcome === 'network') f.fail(new TypeError('temporary failure'));
    else
      f.finish(
        outcome === 'success'
          ? ok(null)
          : Response.json(
              { code: outcome === '401' ? 'UNAUTHORIZED' : 'FORBIDDEN', data: null },
              { status: Number(outcome) },
            ),
      );
  });
  expect(f.client.getQueryData(['profile'])).toEqual(expected);
  expect(f.client.getQueryData(['order-detail', false, 'current-private'])).toEqual({
    message: 'current-private',
  });
  expect(screen.getByTestId('toasts')).toHaveTextContent('[]');
});
