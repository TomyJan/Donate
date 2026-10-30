import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import { UserDialog } from './UserDialog';

vi.mock('../app/context', () => ({
  useApp: () => ({
    t: (key: string) => translate(key, 'en'),
    profile: { id: 'administrator' },
    report: vi.fn(),
    notify: vi.fn(),
  }),
}));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
it('shows account information and opens its orders without embedding or fetching donation history', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  const requests: string[] = [];
  vi.stubGlobal('fetch', async (input: string) => {
    requests.push(input);
    if (input.includes('/admin/order/list'))
      return new Response(
        JSON.stringify({ code: 'OK', data: { items: [], total: 0, page: 1, page_size: 10 } }),
        { status: 200 },
      );
    return new Response(
      JSON.stringify({
        code: 'OK',
        data: {
          id: 'target-user',
          nickname: 'Donor',
          avatar_url: '',
          status: 'active',
          admin: false,
          bindings: [],
          created_at: '2026-10-29T00:00:00Z',
          last_login_at: '2026-10-30T00:00:00Z',
          order_count: 3,
          paid_fen: 100,
        },
      }),
      { status: 200 },
    );
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onViewOrders = vi.fn();
  const onClose = vi.fn();
  try {
    render(
      <QueryClientProvider client={client}>
        <div className="d-app">
          <UserDialog id="target-user" onClose={onClose} onViewOrders={onViewOrders} />
        </div>
      </QueryClientProvider>,
    );
    await screen.findByText('Donor');
    expect(document.querySelector('.user-history')).toBeNull();
    expect(requests.some((path) => path.includes('/admin/order/list'))).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: translate('viewDonations', 'en') }));
    expect(onViewOrders).toHaveBeenCalledWith('target-user');
    expect(onClose).toHaveBeenCalledOnce();
  } finally {
    client.clear();
  }
});
