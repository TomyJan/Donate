import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { StrictMode } from 'react';
import { BrowserRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider } from '../app/context';
import type { Profile } from '../lib/types';
import { Admin } from './Admin';

const task = {
  id: 'task-one',
  kind: 'payment',
  order_id: 'order-one',
  merchant_order_no: 'DONATE-101',
  nickname: 'Alice',
  status: 'failed',
  attempts: 4,
  total_attempts: 8,
  source: 'automatic',
  result: 'query_failed',
  created_at: '2026-10-30T08:00:00Z',
  updated_at: '2026-10-31T08:00:00Z',
  run_at: '2026-10-31T09:00:00Z',
  retryable: true,
};
const profile: Profile = {
  id: 'admin-a',
  nickname: 'Admin',
  avatar_url: '',
  provider: 'oidc',
  admin: true,
  csrf_token: 'csrf',
  expires_at: '2099-01-01T00:00:00Z',
  bindings: [],
};
const clients: QueryClient[] = [];
let client: QueryClient;
let requests: URL[];
let retries: { ids: string[]; idempotency_key: string }[];
let failRetry: boolean;
let failList: boolean;
let failDetail: boolean;
let holdDetail: ((value: Response) => void) | undefined;
let delayDetail: boolean;
let delayRetry: boolean;
let finishRetry: ((value: Response) => void) | undefined;
const ok = (data: unknown) => Response.json({ code: 'OK', data });
beforeEach(() => {
  task.status = 'failed';
  localStorage.clear();
  localStorage.setItem('donate-language', 'en');
  requests = [];
  retries = [];
  failRetry = false;
  failList = false;
  failDetail = false;
  delayDetail = false;
  delayRetry = false;
  finishRetry = undefined;
  holdDetail = undefined;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost');
    requests.push(url);
    if (url.pathname.endsWith('site/info'))
      return ok({
        csrf_token: 'csrf',
        name: 'Donate',
        logo: '',
        providers: [],
        payment_enabled: true,
        version: {},
      });
    if (url.pathname.endsWith('user/profile')) return ok(client.getQueryData(['profile']) ?? profile);
    if (url.pathname.endsWith('admin/overview'))
      return ok({ paid_fen: 0, paid: 0, pending: 0, closed: 0, daily: [], channels: [] });
    if (url.pathname.endsWith('/task/list'))
      return failList
        ? new Response('offline', { status: 503 })
        : ok({ items: [task], total: 41, page: Number(url.searchParams.get('page')), page_size: 20 });
    if (url.pathname.endsWith('/task/overview'))
      return ok({
        total: 41,
        counts: { ready: 20, running: 10, done: 10, failed: 1 },
        due: 12,
        scheduled: 8,
        stale_running: 2,
        recording_since: '2026-10-30T00:00:00Z',
        from: '2026-10-25T00:00:00Z',
        to: '2026-11-01T00:00:00Z',
        trend: [{ date: '2026-10-30', created: 2, started: 1, completed: 1, failed: 0, retried: 0 }],
      });
    if (url.pathname.endsWith('/task/detail')) {
      if (failDetail) return new Response('unavailable', { status: 503 });
      if (delayDetail)
        return new Promise<Response>((resolve) => {
          holdDetail = resolve;
        });
      return ok({ ...task, request_id: 'request-101', recording_since: '2026-10-30T00:00:00Z' });
    }
    if (url.pathname.endsWith('/task/history'))
      return ok({
        items: [
          {
            id: `event-${url.searchParams.get('page')}`,
            event: Number(url.searchParams.get('page')) > 1 ? 'snapshot' : 'failed',
            status: 'failed',
            attempt: 8,
            source: 'automatic',
            result: 'query_failed',
            message: Number(url.searchParams.get('page')) > 1 ? 'Upgrade snapshot' : 'Gateway unavailable',
            created_at: '2026-10-31T08:00:00Z',
          },
        ],
        total: 25,
        page: Number(url.searchParams.get('page')),
        page_size: 10,
      });
    if (url.pathname.endsWith('/task/retry')) {
      retries.push(JSON.parse(String(init?.body)));
      if (delayRetry)
        return new Promise<Response>((resolve) => {
          finishRetry = resolve;
        });
      return failRetry
        ? new Response('unavailable', { status: 503 })
        : ok({ queued: 1, items: [{ id: task.id, result: 'queued' }] });
    }
    throw new Error(`Unexpected request: ${url.pathname}`);
  });
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false }, mutations: { retry: false } },
  });
  clients.push(client);
});
it('offers immediate execution for a scheduled ready task', async () => {
  task.status = 'ready';
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Run now DONATE-101' }));
  expect(await screen.findByRole('dialog', { name: 'Run now' })).toBeInTheDocument();
});
afterEach(() => {
  cleanup();
  for (const item of clients.splice(0)) item.clear();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function openTasks() {
  render(
    <StrictMode>
      <QueryClientProvider client={client}>
        <AppProvider>
          <BrowserRouter>
            <div className="d-app">
              <Admin />
            </div>
          </BrowserRouter>
        </AppProvider>
      </QueryClientProvider>
    </StrictMode>,
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Tasks' }));
  await screen.findByRole('button', { name: 'Task details DONATE-101' });
}
it('shows current queue counts and resets page and selection when filters change', async () => {
  await openTasks();
  expect(screen.getByRole('img', { name: 'Execution trend' })).toBeInTheDocument();
  expect(screen.getByText('20', { selector: '.task-stat strong' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Next' }));
  await waitFor(() =>
    expect(
      requests
        .filter((u) => u.pathname.endsWith('/task/list'))
        .at(-1)
        ?.searchParams.get('page'),
    ).toBe('2'),
  );
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select task DONATE-101' }));
  expect(screen.getByRole('button', { name: 'Retry selected' })).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: 'Search tasks' }), { target: { value: 'Alice' } });
  await waitFor(() =>
    expect(
      requests
        .filter((u) => u.pathname.endsWith('/task/list'))
        .at(-1)
        ?.searchParams.get('page'),
    ).toBe('1'),
  );
  expect(screen.queryByRole('button', { name: 'Retry selected' })).not.toBeInTheDocument();
});
it('loads real detail and paginates execution history', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  const dialog = await screen.findByRole('dialog', { name: 'Task details' });
  expect(await within(dialog).findByText('Gateway unavailable')).toBeInTheDocument();
  expect(within(dialog).getByText('request-101')).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Next' }));
  expect(
    await within(dialog).findByText('Upgrade snapshot', { selector: '.task-event-message' }),
  ).toBeInTheDocument();
  expect(within(dialog).getByText(/History recorded since/)).toBeInTheDocument();
});
it('keeps one retry intent key after network failure and shows per-item results', async () => {
  failRetry = true;
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Retry DONATE-101' }));
  const confirm = await screen.findByRole('dialog', { name: 'Retry task' });
  fireEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));
  await within(confirm).findByRole('alert');
  failRetry = false;
  fireEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));
  expect(await screen.findByRole('dialog', { name: 'Results' })).toHaveTextContent('Queued');
  expect(retries).toHaveLength(2);
  expect(retries[0].idempotency_key).toBe(retries[1].idempotency_key);
  expect(retries[0].ids).toEqual(['task-one']);
});
it('recovers a failed list without resetting filters', async () => {
  await openTasks();
  fireEvent.change(screen.getByRole('textbox', { name: 'Search tasks' }), { target: { value: 'Alice' } });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh tasks' })).not.toBeDisabled());
  failList = true;
  fireEvent.click(screen.getByRole('button', { name: 'Refresh tasks' }));
  await screen.findByText('The server is busy. Please try again shortly.');
  failList = false;
  failDetail = false;
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await waitFor(() =>
    expect(screen.queryByText('The server is busy. Please try again shortly.')).not.toBeInTheDocument(),
  );
  expect(screen.getByRole('textbox', { name: 'Search tasks' })).toHaveValue('Alice');
});
it('clears dialogs after permission loss and ignores late private detail', async () => {
  delayDetail = true;
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  await waitFor(() => expect(holdDetail).toBeTypeOf('function'));
  act(() => {
    client.setQueryData(['profile'], { ...profile, admin: false });
  });
  expect(await screen.findByText('This account cannot access site management.')).toBeInTheDocument();
  await act(async () => {
    holdDetail?.(ok({ ...task, request_id: 'private-request', recording_since: '' }));
  });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.queryByText('private-request')).not.toBeInTheDocument();
});
it('pauses polling while hidden and refreshes only task queries when visible', async () => {
  await openTasks();
  vi.useFakeTimers();
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  fireEvent(document, new Event('visibilitychange'));
  const count = requests.length;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(requests).toHaveLength(count);
  hidden.mockReturnValue(false);
  fireEvent(document, new Event('visibilitychange'));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(requests.length).toBeGreaterThan(count);
  expect(
    requests
      .slice(count)
      .filter((u) => !u.pathname.endsWith('/user/profile'))
      .every((u) => u.pathname.includes('/admin/task/')),
  ).toBe(true);
});
it('revokes an open retry confirmation after a list refresh error', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Retry DONATE-101' }));
  await screen.findByRole('dialog', { name: 'Retry task' });
  failList = true;
  await act(async () => {
    await client.invalidateQueries({ queryKey: ['admin-tasks'] });
  });
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Retry task' })).not.toBeInTheDocument());
  expect(screen.getByRole('button', { name: 'Retry DONATE-101' })).toBeDisabled();
});
it('retains cached detail after a background detail failure and disables its retry', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  const detail = await screen.findByRole('dialog', { name: 'Task details' });
  await within(detail).findByText('request-101');
  fireEvent.click(within(detail).getByRole('button', { name: 'Retry' }));
  await screen.findByRole('dialog', { name: 'Retry task' });
  failDetail = true;
  await act(async () => {
    await client.invalidateQueries({
      predicate: (item) => item.queryKey[0] === 'admin-tasks' && item.queryKey[2] === 'detail',
    });
  });
  await within(detail).findByText('The server is busy. Please try again shortly.');
  expect(within(detail).getByText('request-101')).toBeInTheDocument();
  expect(screen.queryByRole('dialog', { name: 'Retry task' })).not.toBeInTheDocument();
  expect(
    within(detail)
      .getAllByRole('button', { name: 'Retry' })
      .some((button) => button.hasAttribute('disabled')),
  ).toBe(true);
});
it.each([200, 403])(
  'isolates an old retry HTTP %s response after session credentials change',
  async (status) => {
    delayRetry = true;
    await openTasks();
    fireEvent.click(screen.getByRole('button', { name: 'Retry DONATE-101' }));
    const confirm = await screen.findByRole('dialog', { name: 'Retry task' });
    fireEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(finishRetry).toBeTypeOf('function'));
    act(() => {
      client.setQueryData(['profile'], { ...profile, csrf_token: 'replacement-csrf' });
    });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Retry task' })).not.toBeInTheDocument());
    await act(async () => {
      finishRetry?.(
        status === 200
          ? ok({ queued: 1, items: [{ id: task.id, result: 'queued' }] })
          : Response.json({ code: 'FORBIDDEN', data: null }, { status }),
      );
    });
    expect(screen.getByRole('button', { name: 'Tasks' })).toBeInTheDocument();
    expect(client.getQueryData<Profile>(['profile'])?.admin).toBe(true);
    expect(screen.queryByRole('dialog', { name: 'Results' })).not.toBeInTheDocument();
  },
);
