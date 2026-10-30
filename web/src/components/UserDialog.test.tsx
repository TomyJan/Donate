import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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

it('returns to the remaining history page when its last item is deleted', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  let total = 11;
  const pages: number[] = [];
  const reply = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 });
  vi.stubGlobal('fetch', async (input: string) => {
    const url = new URL(input, 'http://localhost');
    if (url.pathname.endsWith('/user/detail'))
      return reply({
        id: 'target-user',
        nickname: 'Donor',
        status: 'active',
        admin: false,
        bindings: [],
        last_login_at: '2026-10-30T00:00:00Z',
      });
    const page = Number(url.searchParams.get('page'));
    pages.push(page);
    const count = Math.max(0, Math.min(10, total - (page - 1) * 10));
    return reply({
      total,
      page,
      page_size: 10,
      items: Array.from({ length: count }, (_, index) => ({
        id: `order-${(page - 1) * 10 + index}`,
        created_at: '2026-10-30T00:00:00Z',
        amount_fen: 100,
        status: 'paid',
      })),
    });
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  try {
    render(
      <QueryClientProvider client={client}>
        <div className="d-app">
          <UserDialog id="target-user" onClose={() => {}} onOrder={() => {}} onViewOrders={() => {}} />
        </div>
      </QueryClientProvider>,
    );
    await screen.findByText('1 / 2');
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await screen.findByText('2 / 2');
    await waitFor(() => expect(document.querySelectorAll('.history-row')).toHaveLength(1));
    total = 10;
    await act(async () => {
      await client.invalidateQueries({ queryKey: ['admin-user-orders'] });
    });
    await screen.findByText('1 / 1');
    await waitFor(() => expect(document.querySelectorAll('.history-row')).toHaveLength(10));
    expect(pages.at(-1)).toBe(1);
  } finally {
    client.clear();
  }
});
