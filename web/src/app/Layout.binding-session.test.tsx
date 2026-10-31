import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { COOKIE_NOTICE_KEY, LEGAL_VERSION } from '../lib/legal';
import type { Profile, Site } from '../lib/types';
import { AppProvider, useApp } from './context';
import { Layout } from './Layout';

const alice: Profile = {
  id: 'alice',
  nickname: 'Alice',
  csrf_token: 'alice-session',
  expires_at: '2099-11-01T00:00:00Z',
  avatar_url: '',
  provider: 'oidc',
  admin: false,
  bindings: [
    {
      id: 'alice-oidc',
      provider: 'oidc',
      nickname: 'Alice',
      avatar_url: '',
      bound_at: '2026-11-01T00:00:00Z',
    },
  ],
};
const bob: Profile = { ...alice, id: 'bob', nickname: 'Bob', csrf_token: 'bob-session' };
const site: Site = {
  csrf_token: 'guest-csrf',
  logo: 'https://example.test/logo.png',
  name: 'Synthetic site',
  payment_enabled: true,
  providers: ['qq', 'oidc'],
  version: { version: '0.1.1', tag: '', commit: 'synthetic', built_at: '' },
};
const ok = (data: unknown) => Response.json({ code: 'OK', data });
const clients: QueryClient[] = [];
function Controls() {
  const app = useApp();
  return (
    <>
      <button type="button" data-testid="open-account" onClick={() => app.setAccountOpen(true)}>
        Open account
      </button>
      <output data-testid="session">{app.profile?.id ?? 'guest'}</output>
    </>
  );
}
beforeEach(() => {
  history.replaceState(null, '', '/');
  localStorage.clear();
  sessionStorage.clear();
  localStorage.setItem('donate-language', 'en');
  localStorage.setItem(COOKIE_NOTICE_KEY, LEGAL_VERSION);
  vi.stubGlobal('scrollTo', vi.fn());
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(async () => {
  cleanup();
  for (const client of clients.splice(0)) {
    await client.cancelQueries();
    client.clear();
  }
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function fixture() {
  let serverProfile: Profile | null = alice;
  let finishBinding: ((response: Response) => void) | undefined;
  const requests: { csrf: string | null; provider: string }[] = [];
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const path = String(input);
    if (path.endsWith('user/profile')) return ok(serverProfile);
    if (path.endsWith('site/info')) return ok(site);
    if (path.endsWith('user/identity/bind')) {
      requests.push({
        csrf: new Headers(init?.headers).get('X-CSRF-Token'),
        provider: JSON.parse(String(init?.body)).provider,
      });
      return new Promise<Response>((resolve) => {
        finishBinding = resolve;
      });
    }
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <MemoryRouter>
          <Controls />
          <Layout />
        </MemoryRouter>
      </AppProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('alice'));
  const open = async () => {
    fireEvent.click(screen.getByTestId('open-account'));
    fireEvent.click(
      within(await screen.findByRole('dialog', { name: 'Account' })).getByRole('button', { name: 'Link' }),
    );
    return screen.findByRole('dialog', { name: 'Confirm linking' });
  };
  await open();
  return {
    client,
    requests,
    open,
    setServerProfile: (value: Profile | null) => {
      serverProfile = value;
    },
    finish: (response: Response) => {
      if (!finishBinding) throw new Error('Binding request not started');
      finishBinding(response);
    },
  };
}

it('dismisses Alice confirmation after focus discovers Bob and permits a fresh Bob confirmation', async () => {
  const f = await fixture();
  f.setServerProfile(bob);
  fireEvent.focus(window);
  await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('bob'));
  expect(screen.queryByRole('dialog', { name: 'Confirm linking' })).not.toBeInTheDocument();
  expect(f.requests).toEqual([]);
  const fresh = await f.open();
  fireEvent.click(within(fresh).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(f.requests).toEqual([{ csrf: bob.csrf_token, provider: 'qq' }]));
  await act(async () => f.finish(ok({ authorization_url: '#binding-authorized' })));
  await waitFor(() => expect(location.hash).toBe('#binding-authorized'));
});

it.each(['switch', 'logout', 'aba'] as const)(
  'rejects a stale confirmation before React rerenders after %s',
  async (mode) => {
    const f = await fixture();
    const confirm = within(screen.getByRole('dialog', { name: 'Confirm linking' })).getByRole('button', {
      name: 'Confirm',
    });
    await act(async () => {
      f.client.setQueryData(['profile'], mode === 'logout' ? null : bob);
      if (mode === 'aba') f.client.setQueryData(['profile'], alice);
      fireEvent.click(confirm);
    });
    expect(f.requests).toEqual([]);
    expect(screen.queryByRole('dialog', { name: 'Confirm linking' })).not.toBeInTheDocument();
    expect(location.hash).toBe('');
  },
);

it.each([200, 401, 403])(
  'ignores late binding HTTP %s after the initiating session changes',
  async (status) => {
    const f = await fixture();
    fireEvent.click(
      within(screen.getByRole('dialog', { name: 'Confirm linking' })).getByRole('button', {
        name: 'Confirm',
      }),
    );
    await waitFor(() => expect(f.requests).toEqual([{ csrf: alice.csrf_token, provider: 'qq' }]));
    await act(async () => {
      f.client.setQueryData(['profile'], bob);
      f.finish(
        status === 200
          ? ok({ authorization_url: '#stale-binding' })
          : Response.json({ code: status === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN', data: null }, { status }),
      );
    });
    expect(f.client.getQueryData(['profile'])).toEqual(bob);
    await waitFor(() => expect(screen.getByTestId('session')).toHaveTextContent('bob'));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(location.hash).toBe('');
  },
);

it('dismisses confirmation when an account ABA is batched without another click', async () => {
  const f = await fixture();
  await act(async () => {
    f.client.setQueryData(['profile'], bob);
    f.client.setQueryData(['profile'], alice);
  });
  expect(screen.queryByRole('dialog', { name: 'Confirm linking' })).not.toBeInTheDocument();
  expect(f.requests).toEqual([]);
});

it('does not revive a pending binding navigation after an account ABA', async () => {
  const f = await fixture();
  fireEvent.click(
    within(screen.getByRole('dialog', { name: 'Confirm linking' })).getByRole('button', { name: 'Confirm' }),
  );
  await waitFor(() => expect(f.requests).toHaveLength(1));
  await act(async () => {
    f.client.setQueryData(['profile'], bob);
    f.client.setQueryData(['profile'], alice);
    f.finish(ok({ authorization_url: '#stale-binding' }));
  });
  expect(f.client.getQueryData(['profile'])).toEqual(alice);
  expect(location.hash).toBe('');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

it('keeps confirmation usable when a profile refresh only changes display data', async () => {
  const f = await fixture();
  await act(async () => f.client.setQueryData(['profile'], { ...alice, nickname: 'Alice refreshed' }));
  fireEvent.click(
    within(screen.getByRole('dialog', { name: 'Confirm linking' })).getByRole('button', { name: 'Confirm' }),
  );
  await waitFor(() => expect(f.requests).toEqual([{ csrf: alice.csrf_token, provider: 'qq' }]));
  await act(async () => f.finish(ok({ authorization_url: '#binding-authorized' })));
  await waitFor(() => expect(location.hash).toBe('#binding-authorized'));
});
