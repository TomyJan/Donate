import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Profile, Site } from '../lib/types';
import { CompleteLogin } from './CompleteLogin';

const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  vi.unstubAllGlobals();
  localStorage.clear();
  history.replaceState(null, '', '/');
});
function LoginState() {
  const { loginOpen, accountOpen } = useApp();
  return (
    <>
      <output>{loginOpen ? 'login-open' : 'login-closed'}</output>
      <output>{accountOpen ? 'account-open' : 'account-closed'}</output>
    </>
  );
}
function fixture(code: string, callback = '', options: { intent?: string; statusError?: boolean } = {}) {
  history.replaceState(null, '', `/auth/complete${callback}`);
  localStorage.setItem('donate-language', 'en');
  let attempts = 0;
  let statusAttempts = 0;
  const replace = vi.fn();
  vi.stubGlobal('location', { search: location.search, replace });
  vi.stubGlobal('fetch', async (path: string) => {
    if (path.endsWith('user/login/status')) {
      statusAttempts += 1;
      if (options.statusError && statusAttempts === 1) throw new TypeError('synthetic connection failure');
      return Response.json({
        code: 'OK',
        data: { stage: 'qq_number', provider: 'qq', intent: options.intent ?? 'login', return_to: '/' },
      });
    }
    if (path.endsWith('user/login/qq/complete')) {
      attempts += 1;
      return attempts === 1
        ? Response.json({ code, message: 'secret avatar picture payload', data: null }, { status: 400 })
        : Response.json({ code: 'OK', data: { return_to: '/?payment=returned&order=test' } });
    }
    throw new Error(`Unexpected request: ${path}`);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  clients.push(client);
  const profile: Profile | null =
    options.intent === 'bind'
      ? {
          id: 'binding-user',
          nickname: 'Binding user',
          csrf_token: 'binding-session',
          expires_at: '2099-11-01T00:00:00Z',
          avatar_url: '',
          provider: 'oidc',
          admin: false,
          bindings: [],
        }
      : null;
  const site: Site = {
    csrf_token: profile?.csrf_token ?? 'guest-csrf',
    providers: ['qq'],
    logo: '',
    name: 'Synthetic site',
    payment_enabled: true,
    version: { version: '1', commit: 'test', tag: '', built_at: '' },
  };
  client.setQueryData(['profile'], profile);
  client.setQueryData(['site'], site);
  render(
    <QueryClientProvider client={client}>
      <AppProvider>
        <LoginState />
        <CompleteLogin />
      </AppProvider>
    </QueryClientProvider>,
  );
  return { replace };
}
it.each(['QQ_NUMBER_INVALID', 'QQ_VERIFICATION_FAILED', 'QQ_VERIFICATION_UNAVAILABLE'])(
  'keeps QQ input usable after %s and allows successful resubmission',
  async (code) => {
    const f = fixture(code);
    const input = await screen.findByRole('textbox');
    fireEvent.change(input, { target: { value: '12345' } });
    fireEvent.submit(input.closest('form') as HTMLFormElement);
    expect(await screen.findByRole('alert')).not.toHaveTextContent(/secret|avatar|picture/i);
    expect(input).toHaveValue('12345');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Continue' })).toBeEnabled());
    fireEvent.submit(input.closest('form') as HTMLFormElement);
    await waitFor(() => expect(f.replace).toHaveBeenCalledWith('/?payment=returned&order=test'));
  },
);
it.each(['AUTH_FLOW_EXPIRED', 'AUTH_FLOW_INVALID', 'AUTH_ATTEMPTS_EXCEEDED', 'AUTH_SESSION_CHANGED'])(
  'requires a fresh authorization after %s instead of resubmitting the stale flow',
  async (code) => {
    fixture(code);
    const input = await screen.findByRole('textbox');
    fireEvent.change(input, { target: { value: '12345' } });
    fireEvent.submit(input.closest('form') as HTMLFormElement);
    await screen.findByRole('alert');
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Sign in again' }));
    expect(screen.getByText('login-open')).toBeInTheDocument();
  },
);
it('uses a fixed callback classification and ignores arbitrary query content', async () => {
  fixture('', '?error=flow_expired&message=secret-avatar-payload');
  expect(await screen.findByRole('alert')).toHaveTextContent(/expired/i);
  expect(screen.getByRole('alert')).not.toHaveTextContent(/secret|avatar|payload/i);
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
});
it('retries a failed status request without starting a different sign-in', async () => {
  fixture('', '', { statusError: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/connect/i);
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByRole('textbox')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.getByText('login-closed')).toBeInTheDocument();
});
it('returns an expired binding to account controls rather than starting a login', async () => {
  fixture('AUTH_FLOW_EXPIRED', '', { intent: 'bind' });
  const input = await screen.findByRole('textbox');
  fireEvent.change(input, { target: { value: '12345' } });
  fireEvent.submit(input.closest('form') as HTMLFormElement);
  fireEvent.click(await screen.findByRole('button', { name: 'Account' }));
  expect(screen.getByText('account-open')).toBeInTheDocument();
  expect(screen.getByText('login-closed')).toBeInTheDocument();
});
it.each(['secret-avatar-payload', '__proto__', 'constructor'])(
  'handles unknown callback %s with a safe fallback',
  async (callback) => {
    fixture('', `?error=${callback}`);
    expect(await screen.findByRole('alert')).toHaveTextContent(/authorization could not be verified/i);
    expect(screen.getByRole('alert')).not.toHaveTextContent(/secret|avatar|payload|constructor|__proto__/i);
  },
);
