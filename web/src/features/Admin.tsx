import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ArrowDown,
  ArrowDownUp,
  ArrowLeft,
  ArrowUp,
  ChartNoAxesColumn,
  ChevronDown,
  Download,
  ListChecks,
  ReceiptText,
  RefreshCw,
  Search,
  Users,
  X,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { useApp } from '../app/context';
import { AccessState } from '../components/AccessState';
import { AdminHeading } from '../components/AdminHeading';
import { AdminLoading } from '../components/AdminLoading';
import { AdminOrderFilters } from '../components/AdminOrderFilters';
import { AdminTable } from '../components/AdminTable';
import { AnimatedHeight } from '../components/AnimatedHeight';
import { SelectField } from '../components/forms';
import { OrderDialog } from '../components/OrderDialog';
import { type OverviewPeriod, OverviewRange } from '../components/OverviewRange';
import { RequestError } from '../components/RequestError';
import { SelectionButton, SelectionGroup } from '../components/SelectionGroup';
import { UserDialog } from '../components/UserDialog';
import { Button, Empty, Menu, Modal, Pagination } from '../components/ui';
import { amountFen } from '../lib/amount';
import { api, downloadExport, money, query } from '../lib/api';
import type { Audit, Metric, Operation, Order, Overview, Page, User } from '../lib/types';

type Tab = 'overview' | 'orders' | 'users' | 'audit';
type Sort = { key: string; direction: 'asc' | 'desc' };
const bulkActions = ['close', 'hide_order', 'show_order', 'hide_message', 'show_message', 'delete'] as const;
export function Admin() {
  const app = useApp(),
    { t, profile, report } = app;
  const client = useQueryClient();
  const operationEpoch = useRef(0);
  const [tab, setTab] = useState<Tab>('overview'),
    [search, setSearch] = useState(''),
    [status, setStatus] = useState(''),
    [channel, setChannel] = useState(''),
    [visibility, setVisibility] = useState(''),
    [action, setAction] = useState(''),
    [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(20),
    [userFilter, setUserFilter] = useState(''),
    [period, setPeriod] = useState<OverviewPeriod>({ range: '30d' }),
    [from, setFrom] = useState(''),
    [to, setTo] = useState(''),
    [minimum, setMinimum] = useState(''),
    [maximum, setMaximum] = useState(''),
    [sort, setSort] = useState<Sort>({ key: 'created_at', direction: 'desc' }),
    [selection, setSelection] = useState<Set<string>>(new Set()),
    [orderID, setOrderID] = useState<string | null>(null),
    [userID, setUserID] = useState<string | null>(null),
    [operation, setOperation] = useState<Operation | null>(null),
    [busy, setBusy] = useState(false);
  const filters = {
    search,
    status,
    channel,
    visibility,
    from,
    to,
    minimum: amountFen(minimum) ?? undefined,
    maximum: amountFen(maximum) ?? undefined,
    sort: sort.key,
    direction: sort.direction,
    page,
    page_size: pageSize,
    user_id: userFilter,
    action,
  };
  const endpoint =
    tab === 'orders' ? 'admin/order/list' : tab === 'users' ? 'admin/user/list' : 'admin/audit/list';
  const list = useQuery({
    queryKey: ['admin-list', tab, filters],
    queryFn: () => api<Page<Order | User | Audit>>(`${endpoint}?${query(filters)}`),
    enabled: !!profile?.admin && tab !== 'overview',
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[1] === tab ? previous : undefined),
    retry: false,
  });
  const op = useQuery({
    queryKey: ['operation', operation?.id],
    queryFn: () => api<Operation>(`admin/operation/detail?id=${operation?.id}`),
    enabled: !!profile?.admin && !!operation?.confirmed,
    refetchInterval: (q) =>
      q.state.data?.items.some((i) => i.status === 'ready' || i.status === 'running') ? 2000 : false,
  });
  const currentOp = op.data ?? operation;
  const closeOperation = () => {
    operationEpoch.current += 1;
    setOperation(null);
    client.removeQueries({ queryKey: ['operation'] });
  };
  // biome-ignore lint/correctness/useExhaustiveDependencies: A changed user or permission revokes administrative dialogs and operations.
  useEffect(() => {
    operationEpoch.current += 1;
    setOperation(null);
    setSelection(new Set());
    setOrderID(null);
    setUserID(null);
  }, [profile?.id, profile?.admin]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: filter changes reset selection and pagination.
  useEffect(() => {
    setPage(1);
    setSelection(new Set());
  }, [search, status, channel, visibility, from, to, minimum, maximum, action, pageSize, userFilter]);
  const navigate = (next: Tab) => {
    if (next === tab) return;
    closeOperation();
    setTab(next);
    setSearch('');
    setStatus('');
    setChannel('');
    setVisibility('');
    setAction('');
    setFrom('');
    setTo('');
    setMinimum('');
    setMaximum('');
    setUserFilter('');
    setPage(1);
    setSelection(new Set());
    setSort({ key: 'created_at', direction: 'desc' });
  };
  const run = async (fn: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      report(e);
    } finally {
      setBusy(false);
    }
  };
  const preview = async (action: string, ids = [...selection]) => {
    const epoch = ++operationEpoch.current;
    const prepared = await api<Operation>('admin/order/action/preview', { action, ids });
    if (epoch === operationEpoch.current) setOperation(prepared);
  };
  const execute = async () => {
    if (!operation) return;
    const epoch = operationEpoch.current;
    const result = await api<Operation>('admin/order/action/execute', { operation_id: operation.id });
    if (epoch === operationEpoch.current) setOperation(result);
    await client.invalidateQueries();
    setSelection(new Set());
    if (result.action === 'export') await downloadExport(result.id);
  };
  useEffect(() => {
    if (list.data)
      setPage((current) => Math.min(current, Math.max(1, Math.ceil(list.data.total / pageSize))));
  }, [list.data, pageSize]);
  const rows = list.data?.items ?? [];
  const toggle = (id: string) =>
    setSelection((old) => {
      const next = new Set(old);
      if (next.has(id)) next.delete(id);
      else if (next.size < 100) next.add(id);
      return next;
    });
  const statusOptions = [
    ['', t(tab === 'users' ? 'allUsers' : 'allStatus')],
    ...(tab === 'users' ? ['active', 'banned'] : ['paid', 'pending', 'closed']).map((k) => [k, t(k)]),
  ] as [string, string][];
  if (app.loading) return <AdminLoading shell />;
  if (!profile?.admin) return <AccessState signedIn={!!profile} onLogin={() => app.setLoginOpen(true)} />;
  return (
    <div className="a-layout">
      <SelectionGroup as="nav" className="a-side" aria-label={t('admin')}>
        {(
          [
            ['overview', ChartNoAxesColumn],
            ['orders', ReceiptText],
            ['users', Users],
            ['audit', ListChecks],
          ] as const
        ).map(([key, Icon]) => (
          <SelectionButton
            key={key}
            className="a-btn a-ghost a-nav-item"
            selected={tab === key}
            aria-current={tab === key ? 'page' : undefined}
            onClick={() => navigate(key)}
          >
            <Icon />
            <span className="a-nav-label">{t(key)}</span>
          </SelectionButton>
        ))}
        <Link className="d-btn d-ghost a-btn" to="/">
          <ArrowLeft />
          {t('home')}
        </Link>
      </SelectionGroup>
      <main key={tab} className="a-main">
        {tab !== 'overview' && (
          <AdminHeading title={t(tab)}>
            {tab === 'orders' && selection.size > 0 && (
              <>
                <span className="a-selection-count">
                  {t('selected')} {selection.size}
                </span>
                <Button
                  className="a-icon d-ghost"
                  aria-label={t('clear')}
                  onClick={() => setSelection(new Set())}
                >
                  <X />
                </Button>
                <Button
                  className="a-icon d-ghost"
                  aria-label={t('export')}
                  disabled={busy}
                  onClick={() => void run(() => preview('export'))}
                >
                  <Download />
                </Button>
                <Menu
                  label={t('batch')}
                  trigger={
                    <Button
                      className="a-batch-trigger"
                      aria-label={t('batch')}
                      title={t('batch')}
                      disabled={busy}
                    >
                      <ListChecks />
                      <span className="a-batch-label">{t('batch')}</span>
                      <ChevronDown />
                    </Button>
                  }
                  items={bulkActions.map((action) => ({
                    label: t(action === 'close' ? 'closeOrder' : action),
                    danger: action === 'delete',
                    action: () => void run(() => preview(action)),
                  }))}
                />
              </>
            )}
          </AdminHeading>
        )}
        {tab === 'overview' ? (
          <OverviewSection period={period} onChange={setPeriod} />
        ) : (
          <>
            {tab === 'orders' ? (
              <AdminOrderFilters
                search={search}
                onSearch={setSearch}
                status={status}
                onStatus={setStatus}
                channel={channel}
                onChannel={setChannel}
                user={userFilter}
                onUser={setUserFilter}
                value={{ from, to, minimum, maximum, visibility }}
                onApply={(next) => {
                  setFrom(next.from);
                  setTo(next.to);
                  setMinimum(next.minimum);
                  setMaximum(next.maximum);
                  setVisibility(next.visibility);
                }}
              />
            ) : (
              <div className="a-tools">
                {tab === 'users' ? (
                  <label className="a-search">
                    <Search />
                    <input
                      aria-label={t('userSearch')}
                      placeholder={t('userSearch')}
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                    />
                  </label>
                ) : (
                  <SelectField
                    label={t('action')}
                    value={action}
                    onValue={setAction}
                    options={[
                      ['', t('allActions')],
                      ['order.', t('ordersAction')],
                      ['user.', t('usersAction')],
                    ]}
                  />
                )}
                {tab === 'users' && (
                  <SelectField
                    label={t('status')}
                    value={status}
                    onValue={setStatus}
                    options={statusOptions}
                  />
                )}
              </div>
            )}
            {list.error && (
              <RequestError error={list.error} busy={list.isFetching} onRetry={() => void list.refetch()} />
            )}
            {list.isLoading ? (
              <AdminLoading kind="table" />
            ) : list.error && !rows.length ? null : !rows.length ? (
              <Empty />
            ) : (
              <AdminTable
                kind={tab}
                rows={rows}
                sort={sort}
                onSort={(next) => {
                  setSort(next);
                  setPage(1);
                }}
                selection={selection}
                onToggle={toggle}
                onToggleAll={() =>
                  setSelection((old) => {
                    const next = new Set(old);
                    if (rows.every((row) => next.has(row.id))) for (const row of rows) next.delete(row.id);
                    else for (const row of rows) if (next.size < 100) next.add(row.id);
                    return next;
                  })
                }
                onDetails={(row) =>
                  tab === 'orders'
                    ? setOrderID(row.id)
                    : tab === 'users'
                      ? setUserID(row.id)
                      : (row as Audit).target_type === 'order'
                        ? setOrderID((row as Audit).target_id)
                        : setUserID((row as Audit).target_id)
                }
                onTarget={(kind, id) => (kind === 'order' ? setOrderID(id) : setUserID(id))}
              />
            )}
            {list.data && (
              <Pagination
                page={page}
                size={pageSize}
                total={list.data.total}
                onPage={(next) => {
                  setPage(next);
                  setSelection(new Set());
                }}
                onSize={setPageSize}
              />
            )}
          </>
        )}
      </main>
      <OrderDialog id={orderID} onClose={() => setOrderID(null)} admin />
      <UserDialog
        id={userID}
        onClose={() => setUserID(null)}
        onViewOrders={(id) => {
          navigate('orders');
          setUserFilter(id);
        }}
      />
      <Modal
        open={!!currentOp}
        dismissible={!busy}
        onOpenChange={(v) => {
          if (!v) {
            closeOperation();
          }
        }}
        title={
          currentOp?.confirmed
            ? t('result')
            : t(currentOp?.action === 'close' ? 'closeOrder' : (currentOp?.action ?? 'confirm'))
        }
      >
        <p>
          {!currentOp?.confirmed &&
            (currentOp?.action === 'delete'
              ? t('deleteHint')
              : currentOp?.action === 'close'
                ? t('closeHint')
                : '')}
        </p>
        <div className="operation-items">
          {currentOp?.items.map((item) => (
            <div key={item.id}>
              <span>
                {item.id.slice(0, 8)}
                {item.amount_fen !== undefined && <small>¥{money(item.amount_fen)}</small>}
              </span>
              <span>{t(currentOp.confirmed ? item.result : (item.status ?? item.result))}</span>
            </div>
          ))}
        </div>
        <div className="dialog-actions">
          <Button
            disabled={busy}
            onClick={() => {
              closeOperation();
              void client.invalidateQueries();
            }}
          >
            {t(currentOp?.confirmed ? 'close' : 'cancel')}
          </Button>
          {!currentOp?.confirmed && (
            <Button
              className={currentOp?.action === 'delete' ? 'is-destructive' : 'd-primary'}
              disabled={busy}
              onClick={() => void run(execute)}
            >
              {t('confirm')}
            </Button>
          )}
        </div>
      </Modal>
    </div>
  );
}
function OverviewSection({
  period,
  onChange,
}: {
  period: OverviewPeriod;
  onChange: (value: OverviewPeriod) => void;
}) {
  const { t, report } = useApp();
  const [lastSuccess, setLastSuccess] = useState<Overview>();
  const overview = useQuery({
    queryKey: ['overview', period],
    queryFn: () => api<Overview>(`admin/overview?${query(period)}`),
    placeholderData: (previous) => previous,
    retry: false,
  });
  useEffect(() => {
    if (overview.isSuccess && !overview.isPlaceholderData) setLastSuccess(overview.data);
  }, [overview.data, overview.isSuccess, overview.isPlaceholderData]);
  useEffect(() => {
    if (overview.error) report(overview.error);
  }, [overview.error, report]);
  const displayed = overview.data ?? lastSuccess;
  return (
    <>
      <AdminHeading title={t('overview')}>
        <OverviewRange value={period} onChange={onChange} />
        <Button
          className="d-ghost d-icon overview-refresh"
          aria-label={t('refreshOverview')}
          title={t('refreshOverview')}
          disabled={overview.isFetching}
          onClick={() => void overview.refetch()}
        >
          <RefreshCw />
        </Button>
      </AdminHeading>
      <section className="overview-panel" aria-busy={overview.isFetching}>
        {overview.isFetching && displayed && (
          <div className="overview-progress" role="progressbar" aria-label={t('loading')} />
        )}
        <AnimatedHeight>
          {displayed ? (
            <OverviewPanel data={displayed} />
          ) : overview.error ? (
            <RequestError
              error={overview.error}
              busy={overview.isFetching}
              onRetry={() => void overview.refetch()}
            />
          ) : (
            <AdminLoading />
          )}
        </AnimatedHeight>
      </section>
    </>
  );
}
function OverviewPanel({ data }: { data: Overview }) {
  const { t } = useApp();
  return (
    <>
      <div className="overview-stats">
        {[
          ['totalPaid', `¥${money(data.paid_fen)}`],
          ['paidOrders', data.paid],
          ['pendingOrders', data.pending],
          ['closedOrders', data.closed],
        ].map(([key, value]) => (
          <div key={key}>
            <span>{t(key as string)}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </div>
      <div className="overview-grid">
        {[
          ['daily', data.daily],
          ['channels', data.channels],
        ].map(([key, metrics]) => (
          <MetricTable key={key as string} title={key as string} metrics={metrics as Metric[]} />
        ))}
      </div>
    </>
  );
}
function MetricTable({ title, metrics }: { title: string; metrics: Metric[] }) {
  const { t } = useApp();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(10);
  const [sort, setSort] = useState<{ key: keyof Metric; asc: boolean }>({ key: 'label', asc: false });
  useEffect(() => {
    setPage((current) => Math.min(current, Math.max(1, Math.ceil(metrics.length / size))));
  }, [metrics.length, size]);
  const columns: [keyof Metric, string][] = [
    ['label', title === 'daily' ? 'time' : 'channel'],
    ['paid_fen', 'totalPaid'],
    ['count', 'paidOrders'],
  ];
  const sorted = [...metrics].sort((a, b) => {
    const x = a[sort.key],
      y = b[sort.key];
    const compare =
      typeof x === 'string' && typeof y === 'string' ? x.localeCompare(y) : Number(x) - Number(y);
    return (sort.asc ? 1 : -1) * (compare || a.label.localeCompare(b.label));
  });
  return (
    <section>
      <h2>{t(title)}</h2>
      <table className="a-table a-summary-table">
        <thead>
          <tr>
            {columns.map(([key, label]) => (
              <th
                key={key}
                aria-sort={sort.key === key ? (sort.asc ? 'ascending' : 'descending') : undefined}
              >
                <button
                  type="button"
                  className="a-sort text-button"
                  onClick={() => {
                    setSort({ key, asc: sort.key === key ? !sort.asc : false });
                    setPage(1);
                  }}
                >
                  {t(label)}
                  {sort.key === key ? sort.asc ? <ArrowUp /> : <ArrowDown /> : <ArrowDownUp />}
                </button>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.slice((page - 1) * size, page * size).map((m) => (
            <tr key={m.label}>
              <td>{title === 'channels' ? t(m.label) : m.label}</td>
              <td>¥{money(m.paid_fen)}</td>
              <td>{m.count}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {!metrics.length ? (
        <Empty />
      ) : (
        metrics.length > 10 && (
          <Pagination page={page} total={metrics.length} size={size} onPage={setPage} onSize={setSize} />
        )
      )}
    </section>
  );
}
