import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppProvider, useApp } from '../app/context';
import type { Profile, Site } from '../lib/types';
import { CompleteLogin } from './CompleteLogin';

const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  vi.unstubAllGlobals();
  localStorage.removeItem('donate-language');
  history.replaceState(null, '', '/');
});
function Account() {
  const { profile } = useApp();
  return <p>{profile?.nickname ?? 'guest'}</p>;
}
it.each(['error-callback', 'banned-callback-zh', 'banned-callback-en', 'missing-flow', 'qq-complete'])(
  'keeps the valid session when the authorization flow expires: %s',
  async (stage) => {
    history.replaceState(
      null,
      '',
      `/auth/complete${stage.startsWith('banned-callback') ? '?error=account_banned' : stage === 'error-callback' ? '?error=authorization' : ''}`,
    );
    localStorage.setItem('donate-language', stage.endsWith('-en') ? 'en' : 'zh');
    const profile: Profile = {
      id: 'replacement-B',
      nickname: 'Replacement B',
      csrf_token: 'csrf-B',
      expires_at: new Date(Date.now() + 30 * 86400000).toISOString(),
      avatar_url: '',
      provider: 'oidc',
      admin: false,
      bindings: [],
    };
    const site: Site = {
      csrf_token: profile.csrf_token,
      logo: '',
      name: 'Synthetic site',
      payment_enabled: true,
      providers: ['oidc', 'qq'],
      version: { version: '1', commit: 'test', tag: '', built_at: '' },
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    clients.push(client);
    client.setQueryData(['profile'], profile);
    client.setQueryData(['site'], site);
    client.setQueryData(['order-detail', false, 'B-private'], { message: 'B-private-message' });
    const requested: string[] = [];
    vi.stubGlobal('fetch', async (path: string) => {
      requested.push(path);
      if (path.endsWith('user/login/status') && stage === 'qq-complete')
        return Response.json({ code: 'OK', data: { stage: 'qq_number', provider: 'qq', return_to: '/' } });
      if (path.endsWith('user/login/status') || path.endsWith('user/login/qq/complete'))
        return Response.json({ code: 'UNAUTHORIZED', data: null }, { status: 401 });
      if (path.endsWith('site/info')) return Response.json({ code: 'OK', data: site });
      if (path.endsWith('user/profile')) return Response.json({ code: 'OK', data: profile });
      throw new Error(`Unexpected request: ${path}`);
    });
    render(
      <QueryClientProvider client={client}>
        <AppProvider>
          <Account />
          <CompleteLogin />
        </AppProvider>
      </QueryClientProvider>,
    );
    if (stage === 'qq-complete') {
      const input = await screen.findByRole('textbox');
      fireEvent.change(input, { target: { value: '12345' } });
      fireEvent.submit(input.closest('form') as HTMLFormElement);
      await waitFor(() => expect(requested).toContain('/api/v1/user/login/qq/complete'));
    } else {
      await waitFor(() => expect(requested).toContain('/api/v1/user/login/status'));
    }
    await act(async () => {});
    if (stage.startsWith('banned-callback')) {
      expect(screen.getByRole('heading')).toHaveTextContent(
        stage.endsWith('-en') ? 'Sign-in failed' : '登录失败',
      );
      expect(screen.getByRole('alert')).toHaveTextContent(
        stage.endsWith('-en')
          ? 'Your account has been banned. Contact the site owner if you have questions.'
          : '您的账号已被封禁，如有疑问请联系站长。',
      );
    }
    expect(screen.getByText('Replacement B')).toBeInTheDocument();
    expect(client.getQueryData(['profile'])).toEqual(profile);
    expect(client.getQueryData(['order-detail', false, 'B-private'])).toEqual({
      message: 'B-private-message',
    });
  },
);
