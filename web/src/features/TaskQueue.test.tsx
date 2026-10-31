import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { StrictMode } from 'react';
import { BrowserRouter } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppProvider } from '../app/context';
import type { Order, Page, Profile, TaskItem, TaskOverview } from '../lib/types';
import { Admin } from './Admin';

const taskStyles = readFileSync('src/features/TaskQueue.css', 'utf8');

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
const originalScroll = Element.prototype.scrollIntoView;
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
let trend: TaskOverview['trend'];
let filterWidth: number;
let resizeFilters: (() => void) | undefined;
const ok = (data: unknown) => Response.json({ code: 'OK', data });
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
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
  trend = [{ date: '2026-10-30', created: 2, started: 1, completed: 1, failed: 0, retried: 0 }];
  filterWidth = 1000;
  resizeFilters = undefined;
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this.classList.contains('task-filters') ? filterWidth : 760;
  });
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(private callback: () => void) {}
      observe(element: Element) {
        if (element.classList.contains('task-filters')) resizeFilters = this.callback;
      }
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
        trend,
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
  Element.prototype.scrollIntoView = originalScroll;
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
  await screen.findAllByRole('button', { name: 'Task details DONATE-101' });
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
  expect(screen.getByRole('button', { name: 'Retry selected' })).toBeDisabled();
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
it('polls automatically without refresh controls or an update footer', async () => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
  await openTasks();
  expect(screen.queryByRole('button', { name: 'Refresh tasks' })).not.toBeInTheDocument();
  expect(screen.queryByRole('checkbox', { name: 'Auto refresh' })).not.toBeInTheDocument();
  expect(document.querySelector('.task-update')).not.toBeInTheDocument();
  const polls = requests.filter((url) => url.pathname.endsWith('/task/list')).length;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5001);
  });
  expect(requests.filter((url) => url.pathname.endsWith('/task/list')).length).toBe(polls + 1);
});
it('explains a period without recorded events and keeps truthful zero data available', async () => {
  trend = [{ date: '2026-10-30', created: 0, started: 0, completed: 0, failed: 0, retried: 0 }];
  await openTasks();
  expect(await screen.findByText('No task events in this period')).toBeInTheDocument();
  expect(screen.getByText(/Recording started:/)).toHaveTextContent('2026');
  expect(screen.queryByRole('img', { name: 'Execution trend' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'View trend data' }));
  const table = screen.getByRole('region', { name: 'View trend data' });
  expect(within(table).getAllByText('0', { selector: 'td' })).toHaveLength(5);
});
it('keeps accessible trend data expanded and its initial curve across polling updates', async () => {
  await openTasks();
  const toggle = screen.getByRole('button', { name: 'View trend data' });
  const panel = document.getElementById(toggle.getAttribute('aria-controls') ?? '');
  expect(toggle).toHaveAttribute('aria-expanded', 'false');
  expect(panel).toHaveAttribute('inert');
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'true');
  expect(panel).not.toHaveAttribute('inert');
  const curve = document.querySelector('.task-trend polyline');
  trend = [{ date: '2026-10-30', created: 2, started: 3, completed: 2, failed: 1, retried: 0 }];
  await act(async () => {
    await client.invalidateQueries({ queryKey: ['admin-tasks'] });
  });
  expect(document.querySelector('.task-trend polyline')).toBe(curve);
  expect(toggle).toHaveAttribute('aria-expanded', 'true');
  fireEvent.click(toggle);
  expect(panel).toHaveAttribute('aria-hidden', 'true');
  expect(panel).toHaveAttribute('inert');
});
it('applies chart sizing only to the trend image and leaves the disclosure icon unscaled', async () => {
  const styles = document.createElement('style');
  styles.textContent = taskStyles;
  document.head.append(styles);
  try {
    await openTasks();
    const charts = document.querySelectorAll('.task-trend > svg[role="img"]');
    expect(charts).toHaveLength(1);
    expect(charts[0]).toBe(screen.getByRole('img', { name: 'Execution trend' }));
    const toggle = screen.getByRole('button', { name: 'View trend data' });
    const icon = toggle.querySelector('svg');
    expect(icon).not.toBeNull();
    expect(toggle.querySelectorAll('svg')).toHaveLength(1);
    expect(window.getComputedStyle(charts[0]).minHeight).toBe('160px');
    expect(window.getComputedStyle(icon as SVGSVGElement).minHeight).not.toBe('160px');
    expect(window.getComputedStyle(icon as SVGSVGElement).marginTop).not.toBe('18px');
  } finally {
    styles.remove();
  }
});
it.each(['created', 'started', 'completed', 'failed', 'retried'] as const)(
  'plots a real %s event even if all other series are zero',
  async (event) => {
    trend = [{ date: '2026-10-30', created: 0, started: 0, completed: 0, failed: 0, retried: 0, [event]: 1 }];
    await openTasks();
    expect(screen.getByRole('img', { name: 'Execution trend' })).toBeInTheDocument();
    expect(screen.queryByText('No task events in this period')).not.toBeInTheDocument();
  },
);
it('shows an empty trend for an empty event series', async () => {
  trend = [];
  await openTasks();
  expect(screen.getByText('No task events in this period')).toBeInTheDocument();
  expect(screen.getByText(/Recording started:/)).toBeInTheDocument();
});
it('keeps bulk retry after search and checked selection out of the heading', async () => {
  await openTasks();
  const retry = screen.getByRole('button', { name: 'Retry selected' });
  expect(retry).toBeDisabled();
  const tools = screen.getByRole('textbox', { name: 'Search tasks' }).closest('.task-tools');
  expect(tools?.children[1]).toContainElement(retry);
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select task DONATE-101' }));
  expect(retry).toBeEnabled();
  expect(screen.getByRole('checkbox', { name: 'Select task DONATE-101' })).toBeChecked();
  expect(
    screen.getByRole('checkbox', { name: 'Select task DONATE-101' }).parentElement?.querySelector('svg'),
  ).not.toBeNull();
  expect(document.querySelector('.task-workspace .a-heading')).not.toContainElement(retry);
});
it.each(['other rows', 'no rows'] as const)(
  'clears off-page selection without changing the view after polling leaves %s',
  async (remaining) => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    filterWidth = 280;
    let items: TaskItem[] = [task];
    const fixtureFetch = globalThis.fetch;
    vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), 'http://localhost');
      if (!url.pathname.endsWith('/task/list')) return fixtureFetch(input, init);
      requests.push(url);
      return ok({
        items,
        total: items.length ? (remaining === 'other rows' ? 41 : 10) : 0,
        page: Number(url.searchParams.get('page')),
        page_size: Number(url.searchParams.get('page_size')),
      });
    });
    await openTasks();
    fireEvent.change(screen.getByRole('textbox', { name: 'Search tasks' }), { target: { value: 'Alice' } });
    fireEvent.click(document.querySelector('.task-stat.task-failed') as HTMLElement);
    fireEvent.click(screen.getByRole('button', { name: 'Recorded executions' }));
    await waitFor(() => expect(client.isFetching({ queryKey: ['admin-tasks'] })).toBe(0));
    if (remaining === 'other rows') {
      fireEvent.click(screen.getByRole('button', { name: 'Next' }));
      await waitFor(() => expect(client.isFetching({ queryKey: ['admin-tasks'] })).toBe(0));
    }
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select task DONATE-101' }));
    items = remaining === 'other rows' ? [{ ...task, id: 'task-two', merchant_order_no: 'DONATE-202' }] : [];
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5001);
    });
    await waitFor(() =>
      expect(screen.queryByRole('checkbox', { name: 'Select task DONATE-101' })).not.toBeInTheDocument(),
    );
    const retry = screen.getByRole('button', { name: 'Retry selected' });
    expect(retry).toBeEnabled();
    if (remaining === 'other rows') {
      const all = screen.getByRole('checkbox', { name: 'Select tasks on this page' });
      fireEvent.click(all);
      fireEvent.click(all);
      expect(screen.getByRole('checkbox', { name: 'Select task DONATE-202' })).not.toBeChecked();
      expect(retry).toBeEnabled();
    } else {
      expect(document.querySelector('.task-table')).not.toBeInTheDocument();
    }
    const expectedPage = remaining === 'other rows' ? '2' : '1';
    const last = requests.filter((url) => url.pathname.endsWith('/task/list')).at(-1);
    expect(last?.searchParams.get('search')).toBe('Alice');
    expect(last?.searchParams.get('status')).toBe('failed');
    expect(last?.searchParams.get('sort')).toBe('total_attempts');
    expect(last?.searchParams.get('direction')).toBe('desc');
    expect(last?.searchParams.get('page')).toBe(expectedPage);
    const listRequests = requests.filter((url) => url.pathname.endsWith('/task/list')).length;
    fireEvent.click(screen.getByRole('button', { name: 'More filters' }));
    fireEvent.click(screen.getByRole('button', { name: 'Clear selection' }));
    expect(retry).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Clear selection' })).not.toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Search tasks' })).toHaveValue('Alice');
    expect(document.querySelector('.task-stat.task-failed')).toHaveAttribute('aria-pressed', 'true');
    await act(async () => {});
    expect(requests.filter((url) => url.pathname.endsWith('/task/list'))).toHaveLength(listRequests);
    const listKeys = client
      .getQueryCache()
      .getAll()
      .filter(
        (entry) =>
          entry.getObserversCount() && entry.queryKey[0] === 'admin-tasks' && entry.queryKey[2] === 'list',
      );
    expect(listKeys).toHaveLength(1);
    expect(listKeys[0].queryKey.slice(-3)).toEqual([
      { key: 'total_attempts', direction: 'desc' },
      Number(expectedPage),
      remaining === 'other rows' ? 20 : 10,
    ]);
  },
);
it('tucks filters progressively into an inert animated panel at narrow widths', async () => {
  await openTasks();
  expect(screen.getByRole('combobox', { name: 'Task type' })).toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Status' })).toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Source' })).toBeInTheDocument();
  act(() => {
    filterWidth = 700;
    resizeFilters?.();
  });
  expect(screen.getByRole('combobox', { name: 'Task type' })).toBeInTheDocument();
  expect(screen.queryByRole('combobox', { name: 'Status' })).not.toBeInTheDocument();
  act(() => {
    filterWidth = 280;
    resizeFilters?.();
  });
  expect(screen.queryByRole('combobox', { name: 'Task type' })).not.toBeInTheDocument();
  const more = screen.getByRole('button', { name: 'More filters' });
  fireEvent.click(more);
  expect(more).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByRole('combobox', { name: 'Status' })).toBeInTheDocument();
  fireEvent.click(more);
  const panel = document.getElementById(more.getAttribute('aria-controls') ?? '');
  expect(panel).toHaveAttribute('aria-hidden', 'true');
  expect(panel).toHaveAttribute('inert');
});
it('places type and status in one title row and retry beside history pagination', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  const detail = await screen.findByRole('dialog', { name: 'Task details' });
  await within(detail).findByText('request-101');
  const title = within(detail).getByRole('heading', { name: 'Payment verification' }).parentElement;
  expect(title).toHaveClass('task-detail-title');
  expect(title).toHaveTextContent('Failed');
  const retry = within(detail).getByRole('button', { name: 'Retry' });
  const footer = retry.closest('.task-detail-footer');
  expect(footer).not.toBeNull();
  expect(footer).toContainElement(within(detail).getByRole('button', { name: 'Next' }));
  expect(footer?.children[0]).toContainElement(retry);
});
it('clears selected tasks on sort and page changes', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select task DONATE-101' }));
  fireEvent.click(screen.getByRole('button', { name: 'Recorded executions' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Retry selected' })).toBeDisabled());
  await waitFor(() => expect(client.isFetching({ queryKey: ['admin-tasks'] })).toBe(0));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select task DONATE-101' }));
  fireEvent.click(screen.getByRole('button', { name: 'Next' }));
  expect(screen.getByRole('button', { name: 'Retry selected' })).toBeDisabled();
});
it('keeps the status filter and status cards in sync and clears selection', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select task DONATE-101' }));
  fireEvent.click(document.querySelector('.task-stat.task-failed') as HTMLElement);
  expect(screen.getByRole('button', { name: 'Retry selected' })).toBeDisabled();
  expect(screen.getByRole('combobox', { name: 'Status' })).toHaveTextContent('Failed');
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Status' }), { key: 'ArrowDown' });
  fireEvent.click(await screen.findByRole('option', { name: 'Ready' }));
  expect(document.querySelector('.task-stat.task-ready')).toHaveAttribute('aria-pressed', 'true');
  expect(document.querySelector('.task-stat.task-failed')).toHaveAttribute('aria-pressed', 'false');
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
it('distinguishes mixed retry outcomes for different tasks on one order', async () => {
  const fixtureFetch = globalThis.fetch;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (path.includes('/task/list?'))
      return ok({
        items: [task, { ...task, id: 'task-two', kind: 'geo' }],
        total: 2,
        page: 1,
        page_size: 20,
      });
    if (path.endsWith('/task/retry'))
      return ok({
        queued: 1,
        items: [
          { id: 'task-one', result: 'queued' },
          { id: 'task-two', result: 'not_applicable' },
        ],
      });
    return fixtureFetch(input, init);
  });
  await openTasks();
  for (const checkbox of screen.getAllByRole('checkbox', { name: 'Select task DONATE-101' }))
    fireEvent.click(checkbox);
  fireEvent.click(screen.getByRole('button', { name: 'Retry selected' }));
  const confirm = await screen.findByRole('dialog', { name: 'Retry selected' });
  fireEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));
  const result = await screen.findByRole('dialog', { name: 'Results' });
  const queued = within(result).getByText('Queued').closest('div');
  const skipped = within(result).getByText('Not applicable').closest('div');
  expect(queued).toHaveTextContent('Payment verification');
  expect(queued).toHaveTextContent('DONATE-101');
  expect(queued).toHaveTextContent('task-one');
  expect(skipped).toHaveTextContent('Location lookup');
  expect(skipped).toHaveTextContent('DONATE-101');
  expect(skipped).toHaveTextContent('task-two');
});
it('retains retry result identity for a detail task no longer on the current list', async () => {
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  const detail = await screen.findByRole('dialog', { name: 'Task details' });
  await within(detail).findByText('request-101');
  act(() => {
    client.setQueriesData<Page<TaskItem>>(
      { predicate: (item) => item.queryKey[0] === 'admin-tasks' && item.queryKey[2] === 'list' },
      (previous) =>
        previous && { ...previous, items: [{ ...task, id: 'other-task', merchant_order_no: 'DONATE-202' }] },
    );
  });
  await waitFor(() => expect(document.querySelector('.task-table tbody')).toHaveTextContent('DONATE-202'));
  fireEvent.click(within(detail).getByRole('button', { name: 'Retry' }));
  const confirm = await screen.findByRole('dialog', { name: 'Retry task' });
  fireEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));
  const result = await screen.findByRole('dialog', { name: 'Results' });
  const queued = within(result).getByText('Queued').closest('div');
  expect(queued).toHaveTextContent('Payment verification');
  expect(queued).toHaveTextContent('DONATE-101');
  expect(queued).toHaveTextContent('task-one');
});
it('recovers a failed list without resetting filters', async () => {
  await openTasks();
  fireEvent.change(screen.getByRole('textbox', { name: 'Search tasks' }), { target: { value: 'Alice' } });
  await waitFor(() => expect(client.isFetching({ queryKey: ['admin-tasks'] })).toBe(0));
  failList = true;
  await act(async () => {
    await client.invalidateQueries({ queryKey: ['admin-tasks'] });
  });
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
it('allows eight-second background task responses to finish across polling ticks', async () => {
  await openTasks();
  await screen.findByText('20', { selector: '.task-stat strong' });
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  const detail = await screen.findByRole('dialog', { name: 'Task details' });
  await within(detail).findByText('request-101');
  await within(detail).findByText('Gateway unavailable');

  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  fireEvent(document, new Event('visibilitychange'));
  vi.useFakeTimers();
  const fixtureFetch = globalThis.fetch;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (!path.includes('/admin/task/')) return fixtureFetch(input, init);
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(resolve, 8000);
      init?.signal?.addEventListener(
        'abort',
        () => {
          window.clearTimeout(timer);
          reject(init.signal?.reason);
        },
        { once: true },
      );
    });
    const response = await fixtureFetch(input, init);
    const body = await response.json();
    if (path.includes('/task/list?')) body.data.items[0].nickname = 'Slow donor';
    if (path.includes('/task/overview?')) body.data.counts.ready = 27;
    if (path.includes('/task/detail?')) body.data.request_id = 'slow-request';
    if (path.includes('/task/history?')) body.data.items[0].message = 'Slow history update';
    return Response.json(body);
  });
  hidden.mockReturnValue(false);
  fireEvent(document, new Event('visibilitychange'));
  // First poll starts at 5s. The 10s poll must share those requests, which
  // resolve at 13s: slower than one poll, still below the API's 15s timeout.
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3001);
  });
  expect(document.querySelector('.task-table tbody')).toHaveTextContent('Slow donor');
  expect(document.querySelector('.task-stat strong')).toHaveTextContent('27');
  expect(within(detail).getByText('slow-request')).toBeInTheDocument();
  expect(within(detail).getByText('Slow history update')).toBeInTheDocument();
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
  failDetail = false;
  await act(async () => {
    await client.invalidateQueries({
      predicate: (item) => item.queryKey[0] === 'admin-tasks' && item.queryKey[2] === 'detail',
    });
  });
  await waitFor(() =>
    expect(
      within(detail).queryByText('The server is busy. Please try again shortly.'),
    ).not.toBeInTheDocument(),
  );
  expect(within(detail).getByText('request-101')).toBeInTheDocument();
  expect(within(detail).getByRole('button', { name: 'Retry' })).toBeEnabled();
});
it('retains cached history through a temporary refresh error and recovers', async () => {
  const fixtureFetch = globalThis.fetch;
  let failed = false;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) =>
    failed && String(input).includes('/task/history?')
      ? new Response('unavailable', { status: 503 })
      : fixtureFetch(input, init),
  );
  await openTasks();
  fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
  const detail = await screen.findByRole('dialog', { name: 'Task details' });
  await within(detail).findByText('Gateway unavailable');
  failed = true;
  await act(async () => {
    await client.invalidateQueries({
      queryKey: ['admin-tasks', `${profile.id}:${profile.csrf_token}`, 'history', task.id],
    });
  });
  await within(detail).findByText('The server is busy. Please try again shortly.');
  expect(within(detail).getByText('Gateway unavailable')).toBeInTheDocument();
  expect(within(detail).getByText('request-101')).toBeInTheDocument();
  expect(within(detail).getByRole('button', { name: 'Next' })).toBeEnabled();
  failed = false;
  await act(async () => {
    await client.invalidateQueries({
      queryKey: ['admin-tasks', `${profile.id}:${profile.csrf_token}`, 'history', task.id],
    });
  });
  await waitFor(() =>
    expect(
      within(detail).queryByText('The server is busy. Please try again shortly.'),
    ).not.toBeInTheDocument(),
  );
  expect(within(detail).getByText('Gateway unavailable')).toBeInTheDocument();
  expect(within(detail).getByRole('button', { name: 'Retry' })).toBeEnabled();
});
it.each(['detail', 'history'])(
  'revokes only deleted task caches after a successful detail receives %s NOT_FOUND',
  async (missingEndpoint) => {
    const fixtureFetch = globalThis.fetch;
    const scope = `${profile.id}:${profile.csrf_token}`;
    const taskRoot = ['admin-tasks', scope];
    const detailKey = [...taskRoot, 'detail', task.id];
    const historyKeys = [
      [...taskRoot, 'history', task.id, 1, 10],
      [...taskRoot, 'history', task.id, 2, 10],
      [...taskRoot, 'history', task.id, 7, 50],
    ];
    const otherKeys = [
      [...taskRoot, 'detail', 'other-task'],
      [...taskRoot, 'history', 'other-task', 1, 10],
      ['admin-tasks', 'other-admin:other-session', 'detail', task.id],
      ['admin-tasks', 'other-admin:other-session', 'history', task.id, 1, 10],
    ];
    const operation = {
      id: 'delete-operation',
      action: 'delete',
      expires_at: '2099-01-01T00:00:00Z',
      items: [{ id: task.order_id, result: 'ready' }],
    };
    const order: Order = {
      id: task.order_id,
      user_id: 'donor-one',
      nickname: 'Alice',
      avatar_url: '',
      amount_fen: 100,
      channel: 'alipay',
      status: 'paid',
      merchant_order_no: task.merchant_order_no,
      api_trade_no: 'api-trade-one',
      trade_no: 'trade-one',
      province: '广东',
      created_at: task.created_at,
      expires_at: '2099-01-01T00:00:00Z',
      protocol: 'v2',
      gateway_status: 'paid',
      remote_close_status: '',
      hidden_order: false,
      hidden_message: false,
      close_reason: '',
      message: 'Original order',
    };
    let deleted = false;
    let finishLate: ((value: Response) => void) | undefined;
    let lateSignal: AbortSignal | null | undefined;
    const missing = () => Response.json({ code: 'NOT_FOUND', data: null }, { status: 404 });
    vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.includes('/admin/order/detail?')) return deleted ? missing() : ok(order);
      if (path.endsWith('/admin/order/action/preview')) return ok(operation);
      if (path.endsWith('/admin/order/action/execute')) {
        deleted = true;
        return ok({ ...operation, items: [{ id: task.order_id, result: 'deleted' }] });
      }
      if (deleted && /\/task\/(detail|history)\?/.test(path)) {
        if (path.includes(`/task/${missingEndpoint}?`)) return missing();
        lateSignal = init?.signal;
        return new Promise<Response>((resolve) => {
          finishLate = resolve;
        });
      }
      if (deleted && path.includes('/task/list?')) return ok({ items: [], total: 0, page: 1, page_size: 20 });
      return fixtureFetch(input, init);
    });
    await openTasks();
    fireEvent.click(screen.getByRole('button', { name: 'Task details DONATE-101' }));
    const detail = await screen.findByRole('dialog', { name: 'Task details' });
    await within(detail).findByText('request-101');
    await within(detail).findByText('Gateway unavailable');
    fireEvent.click(within(detail).getByRole('button', { name: 'Next' }));
    await within(detail).findByText('Upgrade snapshot', { selector: '.task-event-message' });
    act(() => {
      client.setQueryData(historyKeys[2], { items: [{ id: 'old-history-page' }], total: 301 });
      for (const key of otherKeys) client.setQueryData(key, { preserved: key });
    });
    fireEvent.click(within(detail).getByRole('button', { name: 'AliceDONATE-101' }));
    const orderDialog = await screen.findByRole('dialog', { name: 'Order details' });
    fireEvent.click(await within(orderDialog).findByRole('button', { name: 'Delete order' }));
    const confirmation = await screen.findByRole('dialog', { name: 'Delete order' });
    await waitFor(() => expect(within(confirmation).getByRole('button', { name: 'Confirm' })).toBeEnabled());
    fireEvent.click(within(confirmation).getByRole('button', { name: 'Confirm' }));
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Task details' })).not.toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Order details' })).not.toBeInTheDocument(),
    );
    expect(lateSignal?.aborted).toBe(true);
    await act(async () => {
      finishLate?.(
        ok(
          missingEndpoint === 'history'
            ? { ...task, request_id: 'late-deleted-detail' }
            : { items: [{ id: 'late-event', message: 'Late deleted history' }], total: 1 },
        ),
      );
    });
    expect(screen.queryByText('request-101')).not.toBeInTheDocument();
    expect(
      screen.queryByText('Upgrade snapshot', { selector: '.task-event-message' }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText('late-deleted-detail')).not.toBeInTheDocument();
    expect(screen.queryByText('Late deleted history')).not.toBeInTheDocument();
    for (const key of [detailKey, ...historyKeys]) expect(client.getQueryData(key)).toBeUndefined();
    for (const key of otherKeys) expect(client.getQueryData(key)).toEqual({ preserved: key });
  },
);
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

import { readFileSync } from 'node:fs';
