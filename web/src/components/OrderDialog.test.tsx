import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import type { Order } from '../lib/types';
import { OrderDialog, orderCopyText } from './OrderDialog';

const { report, notify, session } = vi.hoisted(() => ({
  report: vi.fn(),
  notify: vi.fn(),
  session: { profile: null as { id: string; csrf_token: string } | null },
}));
vi.mock('../app/context', () => ({
  useApp: () => ({ t: (key: string) => translate(key, 'en'), report, notify, profile: session.profile }),
}));
const clients: QueryClient[] = [];
beforeEach(() => {
  session.profile = null;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  report.mockClear();
  notify.mockClear();
});
function renderOrder(admin = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const view = render(
    <QueryClientProvider client={client}>
      <div className="d-app">
        <OrderDialog id="order-uuid" onClose={() => {}} admin={admin} />
      </div>
    </QueryClientProvider>,
  );
  return { ...view, client };
}
const response = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 });

function rerenderOrder(view: ReturnType<typeof renderOrder>, id: string | null = 'order-uuid') {
  view.rerender(
    <QueryClientProvider client={view.client}>
      <div className="d-app">
        <OrderDialog id={id} onClose={() => {}} admin />
      </div>
    </QueryClientProvider>,
  );
}
const confirmation = {
  id: 'hide-preview',
  action: 'hide_order',
  expires_at: '2099-01-01T00:00:00Z',
  items: [{ id: 'order-uuid', result: 'ready' }],
};

it.each([
  ['preview', 401, 'credentials'],
  ['preview', 403, 'credentials'],
  ['execute', 401, 'credentials'],
  ['execute', 403, 'credentials'],
  ['reconcile', 401, 'credentials'],
  ['reconcile', 403, 'credentials'],
  ['execute', 403, 'unmount'],
  ['reconcile', 401, 'unmount'],
] as const)('ignores an old %s HTTP %s denial after %s', async (endpoint, status, boundary) => {
  session.profile = { id: 'alice', csrf_token: 'old-session' };
  let finish: ((value: Response) => void) | undefined;
  vi.stubGlobal('fetch', async (input: string) => {
    const path = String(input);
    if (path.endsWith(`/${endpoint}`))
      return new Promise<Response>((resolve) => {
        finish = resolve;
      });
    if (path.endsWith('/preview')) return response(confirmation);
    return response({ ...order, protocol: 'v2' });
  });
  const invalid = vi.fn(),
    forbidden = vi.fn();
  window.addEventListener('session-invalid', invalid);
  window.addEventListener('admin-forbidden', forbidden);
  try {
    const view = renderOrder(true);
    if (endpoint === 'reconcile')
      fireEvent.click(await screen.findByRole('button', { name: 'Reconcile payment' }));
    else {
      fireEvent.click(await screen.findByRole('button', { name: 'Hide order' }));
      if (endpoint === 'execute') {
        await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled());
        fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
      }
    }
    await waitFor(() => expect(finish).toBeTypeOf('function'));
    if (boundary === 'unmount') view.unmount();
    else {
      session.profile = { id: 'alice', csrf_token: 'replacement-session' };
      rerenderOrder(view);
      await screen.findByRole('button', { name: 'Hide order' });
    }
    expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument();
    await act(async () => {
      finish?.(
        Response.json({ code: status === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN', data: null }, { status }),
      );
    });
    expect(invalid).not.toHaveBeenCalled();
    expect(forbidden).not.toHaveBeenCalled();
    expect(report).not.toHaveBeenCalled();
    expect(notify).not.toHaveBeenCalled();
    if (boundary !== 'unmount') expect(screen.getByRole('button', { name: 'Hide order' })).toBeEnabled();
  } finally {
    window.removeEventListener('session-invalid', invalid);
    window.removeEventListener('admin-forbidden', forbidden);
  }
});

it.each([
  ['execute', 'credentials'],
  ['reconcile', 'credentials'],
  ['execute', 'reopen'],
  ['reconcile', 'reopen'],
] as const)('ignores old successful %s effects after %s', async (endpoint, boundary) => {
  session.profile = { id: 'alice', csrf_token: 'old-session' };
  let finish: ((value: Response) => void) | undefined;
  vi.stubGlobal('fetch', async (input: string) => {
    const path = String(input);
    if (path.endsWith(`/${endpoint}`))
      return new Promise<Response>((resolve) => {
        finish = resolve;
      });
    if (path.endsWith('/preview')) return response(confirmation);
    return response({ ...order, protocol: 'v2' });
  });
  const view = renderOrder(true);
  if (endpoint === 'reconcile')
    fireEvent.click(await screen.findByRole('button', { name: 'Reconcile payment' }));
  else {
    fireEvent.click(await screen.findByRole('button', { name: 'Hide order' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled());
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  }
  await waitFor(() => expect(finish).toBeTypeOf('function'));
  if (boundary === 'credentials') session.profile = { id: 'bob', csrf_token: 'replacement-session' };
  else rerenderOrder(view, null);
  rerenderOrder(view);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Hide order' })).toBeEnabled());
  const invalidate = vi.spyOn(view.client, 'invalidateQueries');
  const refetch = vi.spyOn(view.client, 'refetchQueries');
  await act(async () => {
    finish?.(response(confirmation));
  });
  expect(notify).not.toHaveBeenCalled();
  expect(report).not.toHaveBeenCalled();
  expect(invalidate).not.toHaveBeenCalled();
  expect(refetch).not.toHaveBeenCalled();
});

it.each(['bob', 'alice'])('isolates owner details after switching credentials to %s', async (id) => {
  session.profile = { id: 'alice', csrf_token: 'old-session' };
  let requests = 0;
  let oldSignal: AbortSignal | null | undefined;
  vi.stubGlobal('fetch', async (_path: string, init?: RequestInit) => {
    if (++requests === 1) return response({ ...order, ip: '192.0.2.17' });
    if (requests === 2) oldSignal = init?.signal;
    return new Promise<Response>(() => {});
  });
  const view = renderOrder();
  await screen.findByText('192.0.2.17');
  await act(async () => {
    void view.client.invalidateQueries({ queryKey: ['order-detail'] });
  });
  await waitFor(() => expect(requests).toBe(2));
  session.profile = { id, csrf_token: 'replacement-session' };
  view.rerender(
    <QueryClientProvider client={view.client}>
      <div className="d-app">
        <OrderDialog id="order-uuid" onClose={() => {}} />
      </div>
    </QueryClientProvider>,
  );
  expect(screen.queryByText('192.0.2.17')).not.toBeInTheDocument();
  expect(oldSignal?.aborted).toBe(true);
});

it.each([false, true])(
  'preserves an order confirmation across automatic polling (delayed preview=%s)',
  async (delayedPreview) => {
    vi.useFakeTimers();
    let detailRequests = 0;
    let finishPreview: ((value: Response) => void) | undefined;
    const executed: unknown[] = [];
    const operation = {
      id: 'delete-preview',
      action: 'delete',
      expires_at: '2099-01-01T00:00:00Z',
      items: [{ id: 'order-uuid', result: 'ready' }],
    };
    vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith('/reconcile')) return response({});
      if (path.endsWith('/preview')) {
        if (!delayedPreview) return response(operation);
        return new Promise<Response>((resolve) => {
          finishPreview = resolve;
        });
      }
      if (path.endsWith('/execute')) {
        executed.push(JSON.parse(String(init?.body)));
        return response({ ...operation, items: [{ id: 'order-uuid', result: 'deleted' }] });
      }
      if (path.includes('/order/detail?')) {
        detailRequests += 1;
        return new Promise<Response>((resolve) => {
          setTimeout(() => resolve(response({ ...order, protocol: 'v2' })), 100);
        });
      }
      throw new Error(`Unexpected request: ${path}`);
    });
    const advance = async (milliseconds: number) => {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(milliseconds);
      });
    };
    renderOrder(true);
    await advance(200);
    fireEvent.click(screen.getByRole('button', { name: 'Reconcile payment' }));
    await advance(200);
    fireEvent.click(screen.getByRole('button', { name: 'Delete order' }));
    await advance(10);
    if (delayedPreview) expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
    else expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled();

    await advance(2100);
    expect(detailRequests).toBeGreaterThanOrEqual(3);
    expect(screen.queryByRole('button', { name: 'Confirm' })).toBeInTheDocument();
    expect(executed).toEqual([]);
    if (delayedPreview) {
      await act(async () => {
        finishPreview?.(response(operation));
      });
      await advance(10);
    }
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await advance(200);
    expect(executed).toEqual([{ operation_id: 'delete-preview' }]);
    expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument();
  },
);

it('revokes an already-open destructive confirmation when the detail refresh fails', async () => {
  let failed = false;
  const executed: string[] = [];
  vi.stubGlobal('fetch', async (input: string) => {
    const path = String(input);
    if (path.endsWith('/preview'))
      return response({
        id: 'delete-preview',
        action: 'delete',
        expires_at: '2099-01-01T00:00:00Z',
        items: [{ id: 'order-uuid', result: 'ready' }],
      });
    if (path.endsWith('/execute')) {
      executed.push(path);
      return response({});
    }
    return failed ? new Response('unavailable', { status: 503 }) : response(order);
  });
  const { client } = renderOrder(true);
  fireEvent.click(await screen.findByRole('button', { name: 'Delete order' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm' })).toBeEnabled());
  failed = true;
  await act(async () => {
    await client.invalidateQueries({ queryKey: ['order-detail'] });
  });
  await screen.findByText(translate('serverBusyError', 'en'));
  expect(screen.getByRole('button', { name: 'Delete order' })).toBeDisabled();
  expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument();
  expect(executed).toEqual([]);
});

const order: Order & { province: string } = {
  id: 'order-uuid',
  user_id: 'full-user-uuid',
  nickname: 'Admin',
  avatar_url: '',
  amount_fen: 988,
  channel: 'alipay',
  status: 'paid',
  merchant_order_no: 'merchant-123',
  created_at: '2026-10-30T00:00:00Z',
  paid_at: '2026-10-30T00:01:00Z',
  expires_at: '2026-10-30T00:30:00Z',
  api_trade_no: 'private-channel-trade',
  trade_no: 'gateway-trade',
  protocol: 'secret-protocol',
  gateway_status: 'paid',
  remote_close_status: '',
  hidden_order: false,
  hidden_message: false,
  close_reason: '',
  message: '第一行留言\nSecond line\n  keep this indentation',
  ip: '192.0.2.123',
  province: '广东',
};

it.each([
  ['NOT_FOUND', 200, 'recordMissingError'],
  ['FORBIDDEN', 403, 'forbidden'],
  ['UNAUTHORIZED', 401, 'unauthorized'],
  ['ACCOUNT_BANNED', 403, 'banned'],
])('removes cached order details and actions after %s', async (code, status, label) => {
  let missing = false;
  vi.stubGlobal('fetch', async () =>
    missing
      ? new Response(JSON.stringify({ code, data: null }), { status: Number(status) })
      : response({ ...order, status: 'pending', expires_at: '2099-01-01T00:00:00Z' }),
  );
  const { client } = renderOrder();
  expect(await screen.findByRole('button', { name: 'Continue payment' })).toBeEnabled();
  missing = true;
  await act(async () => {
    await client.invalidateQueries({ queryKey: ['order-detail'] });
  });
  await screen.findByText(translate(String(label), 'en'));
  await waitFor(() =>
    expect(client.getQueryData(['order-detail', false, 'order-uuid', 'guest'])).toBeUndefined(),
  );
  expect(screen.queryByText('Admin')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Continue payment' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Copy info' })).not.toBeInTheDocument();
});

it.each([false, true])(
  'keeps an invalidated order unavailable through later network errors until fresh success (admin=%s)',
  async (admin) => {
    let state = 'initial';
    vi.stubGlobal('fetch', async () => {
      if (state === 'missing') return new Response(JSON.stringify({ code: 'NOT_FOUND', data: null }));
      if (state === 'offline') return new Response('proxy unavailable', { status: 503 });
      return response({ ...order, nickname: state === 'recovered' ? 'Fresh donor' : order.nickname });
    });
    const { client } = renderOrder(admin);
    await screen.findByText('Admin');
    state = 'missing';
    await act(async () => {
      await client.invalidateQueries({ queryKey: ['order-detail'] });
    });
    await screen.findByText(translate('recordMissingError', 'en'));
    await waitFor(() =>
      expect(client.getQueryData(['order-detail', admin, 'order-uuid', 'guest'])).toBeUndefined(),
    );
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    state = 'offline';
    await act(async () => {
      await client.invalidateQueries({ queryKey: ['order-detail'] });
    });
    await screen.findByText(translate('serverBusyError', 'en'));
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: admin ? 'Copy ID' : 'Copy info' })).not.toBeInTheDocument();
    state = 'recovered';
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await screen.findByText('Fresh donor');
    expect(screen.queryByText(translate('serverBusyError', 'en'))).not.toBeInTheDocument();
  },
);

it.each([false, true])(
  'shows cached detail refresh failures and recovers on retry (admin=%s)',
  async (admin) => {
    let failed = false;
    vi.stubGlobal('fetch', async () =>
      failed
        ? new Response('proxy unavailable', { status: 503 })
        : response({ ...order, status: 'pending', expires_at: '2099-01-01T00:00:00Z' }),
    );
    const { client } = renderOrder(admin);
    await screen.findByText('Admin');
    failed = true;
    await act(async () => {
      await client.invalidateQueries({ queryKey: ['order-detail'] });
    });
    expect(screen.getByText('Admin')).toBeInTheDocument();
    await screen.findByText(translate('serverBusyError', 'en'));
    expect(
      screen.getByRole('button', { name: admin ? 'Reconcile payment' : 'Continue payment' }),
    ).toBeDisabled();
    failed = false;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() =>
      expect(screen.queryByText(translate('serverBusyError', 'en'))).not.toBeInTheDocument(),
    );
    expect(
      screen.getByRole('button', { name: admin ? 'Reconcile payment' : 'Continue payment' }),
    ).toBeEnabled();
  },
);

it('copies a complete readable order summary using an explicit safe field list', () => {
  const labels: Record<string, string> = {
    orderInfoTitle: '捐赠 TomyJan donate.tomys.top 订单信息:',
    accountLabel: '账号',
    amountLabel: '金额',
    paymentMethod: '付款方式',
  };
  const text = orderCopyText(order, (key) => labels[key] ?? translate(key, 'zh-CN'));
  expect(text).toBe(
    '捐赠 TomyJan donate.tomys.top 订单信息:\n账号: Admin (full-user-uuid)\n金额: ￥9.88\n付款方式: 支付宝\n订单 ID: order-uuid\n商户订单号: merchant-123\n平台流水: gateway-trade\n状态: 已支付\n创建时间: 2026-10-30 08:00\n支付时间: 2026-10-30 08:01\n留言:\n第一行留言\nSecond line\n  keep this indentation',
  );
  expect(text).not.toContain(order.ip);
  expect(text).not.toContain(order.api_trade_no);
  expect(text).not.toContain(order.protocol);
  expect(text).toContain(order.message);
});

it('uses translated labels and channel names for an English order summary', () => {
  const labels: Record<string, string> = {
    orderInfoTitle: 'TomyJan donation · donate.tomys.top · Order information:',
    accountLabel: 'Account',
    amountLabel: 'Amount',
    paymentMethod: 'Payment method',
  };
  const text = orderCopyText({ ...order, paid_at: null }, (key) => labels[key] ?? translate(key, 'en'));
  expect(text).toContain('Account: Admin (full-user-uuid)\nAmount: ￥9.88\nPayment method: Alipay');
  expect(text).toContain('Status: Paid');
  expect(text).toContain('Gateway trade: gateway-trade');
  expect(text).toContain('Message:\n第一行留言\nSecond line\n  keep this indentation');
  expect(text).not.toContain('Paid at:');
});

it('places the frontend copy summary beside the amount and excludes administrative metadata', async () => {
  let clipboard = '';
  vi.stubGlobal('fetch', async () => response(order));
  vi.stubGlobal('navigator', {
    clipboard: {
      writeText: async (value: string) => {
        clipboard = value;
      },
    },
  });
  renderOrder();
  const copy = await screen.findByRole('button', { name: 'Copy info' });
  expect(copy.closest('.order-amount-row')).toHaveTextContent('¥9.88');
  expect(screen.queryByRole('button', { name: 'Copy ID' })).not.toBeInTheDocument();
  expect(screen.getByText('Order IP').closest('div')).toHaveTextContent('192.0.2.123');
  fireEvent.click(copy);
  await act(async () => {});
  expect(clipboard).toContain('Account: Admin (full-user-uuid)');
  expect(clipboard).toContain('Payment method: Alipay');
  expect(clipboard).toContain('Gateway trade: gateway-trade');
  expect(clipboard).toContain(order.message);
  expect(clipboard).not.toContain('192.0.2.123');
  expect(clipboard).not.toContain('private-channel-trade');
  expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument();
  expect(notify).not.toHaveBeenCalled();
});

it('shows only the safe province on the frontend and leaves no empty action footer', async () => {
  vi.stubGlobal('fetch', async () =>
    response({ ...order, location: { province: '广东', city: 'private-city', street: 'private-street' } }),
  );
  renderOrder();
  await screen.findByRole('button', { name: 'Copy info' });
  expect(screen.getByText('Location').closest('div')).toHaveTextContent('广东');
  expect(screen.queryByText('private-city')).not.toBeInTheDocument();
  expect(screen.queryByText('private-street')).not.toBeInTheDocument();
  expect(document.querySelector('.detail-actions')).not.toBeInTheDocument();
});

it('queues a location refresh from the order actions and shows its task feedback', async () => {
  const actions: unknown[] = [];
  let refreshed = false;
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const path = String(input);
    if (path.endsWith('/preview')) {
      actions.push(JSON.parse(String(init?.body)));
      return response({
        id: 'geo-preview',
        action: 'refresh_location',
        expires_at: '2099-01-01T00:00:00Z',
        items: [{ id: order.id, result: 'ready', status: 'paid' }],
      });
    }
    if (path.endsWith('/execute')) {
      actions.push(JSON.parse(String(init?.body)));
      refreshed = true;
      return response({
        id: 'geo-preview',
        action: 'refresh_location',
        items: [{ id: order.id, result: 'queued' }],
      });
    }
    return response({
      ...order,
      jobs: refreshed
        ? [
            {
              id: 'geo-refresh',
              kind: 'geo',
              source: 'admin_refresh',
              status: 'ready',
              result: '',
              attempts: 0,
              created_at: '2026-10-31T00:00:00Z',
              run_at: '2026-10-31T00:00:00Z',
            },
          ]
        : [],
    });
  });
  renderOrder(true);
  fireEvent.click(await screen.findByRole('button', { name: 'Refresh location' }));
  const confirm = await screen.findByRole('button', { name: 'Confirm' });
  await waitFor(() => expect(confirm).toBeEnabled());
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(actions).toEqual([
      { action: 'refresh_location', ids: ['order-uuid'] },
      { operation_id: 'geo-preview' },
    ]),
  );
  await waitFor(() =>
    expect(document.querySelector('.job-list')).toHaveTextContent('Refresh location 2026-10-31 08:00'),
  );
  expect(notify).toHaveBeenCalledWith('Queued');
});

it('keeps an unknown province state visible when geolocation has no province', async () => {
  vi.stubGlobal('fetch', async () => response({ ...order, province: '' }));
  renderOrder();
  await screen.findByRole('button', { name: 'Copy info' });
  expect(screen.getByText('Location').closest('div')).toHaveTextContent('Location unavailable');
});

it('shows every non-geolocation task with its creation time rather than its next run time', async () => {
  const jobs = Array.from({ length: 5 }, (_, index) => ({
    id: `job-${index}`,
    kind: 'reconcile',
    attempts: 1,
    result: `result-${index}`,
    status: 'done',
    created_at: `2026-10-30T00:0${index}:00Z`,
    run_at: '2026-10-31T00:00:00Z',
  }));
  vi.stubGlobal('fetch', async () =>
    response({ ...order, jobs: [...jobs, { ...jobs[0], id: 'geo-job', kind: 'geo' }] }),
  );
  renderOrder(true);
  await screen.findByRole('button', { name: 'Reconcile payment' });
  const rows = document.querySelectorAll('.job-list > div');
  expect(rows).toHaveLength(5);
  expect(rows[0]).toHaveTextContent('Payment verification 2026-10-30 08:00');
  expect(rows[4]).toHaveTextContent('Payment verification 2026-10-30 08:04');
  expect(document.querySelector('.job-list')).not.toHaveTextContent('2026-10-31');
});

it('shows every manual reconciliation request separately when the backend reuses one active job', async () => {
  const reconciliations = Array.from({ length: 5 }, (_, index) => ({
    id: `request-${index}`,
    job_id: 'shared-job',
    created_at: `2026-10-30T00:0${index}:00Z`,
    status: 'ready',
    result: '',
  }));
  const jobs = [
    {
      id: 'shared-job',
      kind: 'reconcile',
      source: 'manual',
      attempts: 0,
      created_at: '2026-10-29T00:00:00Z',
      run_at: '2026-10-31T00:00:00Z',
      status: 'ready',
      result: '',
    },
  ];
  vi.stubGlobal('fetch', async () => response({ ...order, reconciliations, jobs }));
  renderOrder(true);
  await screen.findByRole('button', { name: 'Reconcile payment' });
  const rows = document.querySelectorAll('.job-list > div');
  expect(rows).toHaveLength(5);
  expect(rows[0]).toHaveTextContent('Reconcile payment 2026-10-30 08:00');
  expect(rows[4]).toHaveTextContent('Reconcile payment 2026-10-30 08:04');
  expect(document.querySelector('.job-list')).not.toHaveTextContent('2026-10-29');
});

it('previews a single order before executing its exact operation from a nearby confirmation', async () => {
  let finishPreview: ((response: Response) => void) | undefined;
  const requests: { path: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const path = String(input);
    if (!init?.body) return response(order);
    const body = JSON.parse(String(init.body));
    requests.push({ path, body });
    if (path.endsWith('/preview'))
      return new Promise<Response>((resolve) => {
        finishPreview = resolve;
      });
    return response({
      id: 'operation-123',
      action: 'hide_order',
      items: [{ id: order.id, result: 'updated' }],
    });
  });
  renderOrder(true);
  fireEvent.click(await screen.findByRole('button', { name: 'Hide order' }));
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  expect(document.querySelectorAll('.d-overlay')).toHaveLength(1);
  expect(document.querySelector('.action-confirm')).toBeInTheDocument();
  expect(requests).toEqual([
    { path: '/api/v1/admin/order/action/preview', body: { action: 'hide_order', ids: ['order-uuid'] } },
  ]);
  await act(async () => {
    finishPreview?.(
      response({
        id: 'operation-123',
        action: 'hide_order',
        expires_at: new Date(Date.now() + 60000).toISOString(),
        items: [{ id: order.id, result: 'ready' }],
      }),
    );
  });
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(document.querySelector('.action-confirm')).not.toBeInTheDocument());
  expect(requests).toEqual([
    { path: '/api/v1/admin/order/action/preview', body: { action: 'hide_order', ids: ['order-uuid'] } },
    { path: '/api/v1/admin/order/action/execute', body: { operation_id: 'operation-123' } },
  ]);
});

it('refuses a preview credential that belongs to a different order', async () => {
  const executed: string[] = [];
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    const path = String(input);
    if (!init?.body) return response(order);
    if (path.endsWith('/execute')) executed.push(path);
    return response({
      id: 'wrong-operation',
      action: 'hide_order',
      expires_at: new Date(Date.now() + 60000).toISOString(),
      items: [{ id: 'another-order', result: 'ready' }],
    });
  });
  renderOrder(true);
  fireEvent.click(await screen.findByRole('button', { name: 'Hide order' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Action failed');
  expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
  expect(executed).toEqual([]);
  expect(report).toHaveBeenCalledOnce();
});

it.each([false, true])(
  'shows active visibility flags beside the amount as noninteractive statuses (admin=%s)',
  async (admin) => {
    vi.stubGlobal('fetch', async () => response({ ...order, hidden_order: true, hidden_message: true }));
    renderOrder(admin);
    await screen.findByText('Admin');
    const amountRow = document.querySelector('.order-amount-row') as HTMLElement;
    const statuses = within(amountRow).getByRole('group', { name: 'Visibility' });
    expect(within(statuses).getByText('Order hidden')).toBeInTheDocument();
    expect(within(statuses).getByText('Message hidden')).toBeInTheDocument();
    expect(within(statuses).queryByRole('button')).not.toBeInTheDocument();
    expect(within(statuses).queryByRole('tab')).not.toBeInTheDocument();
    if (!admin) expect(within(amountRow).getByRole('button', { name: 'Copy info' })).toBeInTheDocument();
  },
);

it('does not add false visibility flags or an empty status group', async () => {
  vi.stubGlobal('fetch', async () => response(order));
  renderOrder(true);
  await screen.findByText('Admin');
  expect(
    within(document.querySelector('.order-amount-row') as HTMLElement).queryByRole('group', {
      name: 'Visibility',
    }),
  ).not.toBeInTheDocument();
});
