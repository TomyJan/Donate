import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import type { Order } from '../lib/types';
import { OrderDialog, orderCopyText } from './OrderDialog';

const { report, notify } = vi.hoisted(() => ({ report: vi.fn(), notify: vi.fn() }));
vi.mock('../app/context', () => ({
  useApp: () => ({ t: (key: string) => translate(key, 'en'), report, notify }),
}));
const clients: QueryClient[] = [];
beforeEach(() => {
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
  vi.unstubAllGlobals();
  report.mockClear();
  notify.mockClear();
});
function renderOrder(admin = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <div className="d-app">
        <OrderDialog id="order-uuid" onClose={() => {}} admin={admin} />
      </div>
    </QueryClientProvider>,
  );
}
const response = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 });

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
  expect(screen.queryByText('Order IP')).not.toBeInTheDocument();
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
