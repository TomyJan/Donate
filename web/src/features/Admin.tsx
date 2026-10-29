import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ArrowDown,
  ArrowDownUp,
  ArrowLeft,
  ArrowUp,
  ChartNoAxesColumn,
  ChevronDown,
  Download,
  Ellipsis,
  ListChecks,
  ReceiptText,
  Search,
  SlidersHorizontal,
  Users,
  X,
} from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { useApp } from '../app/context';
import { ConstrainedInput } from '../components/ConstrainedInput';
import { DateTimeField, SelectField } from '../components/forms';
import { OrderDialog } from '../components/OrderDialog';
import { Avatar, Button, Empty, Menu, Modal, Pagination } from '../components/ui';
import { acceptAmountEdit, amountFen } from '../lib/amount';
import { api, dateTime, downloadExport, money, query } from '../lib/api';
import type { Audit, Metric, Operation, Order, Overview, Page, User } from '../lib/types';

type Tab = 'overview' | 'orders' | 'users' | 'audit';
type Sort = { key: string; direction: 'asc' | 'desc' };
const bulkActions = ['close', 'hide_order', 'show_order', 'hide_message', 'show_message', 'delete'] as const;
export function Admin() {
  const app = useApp(),
    { t, profile, report, notify } = app;
  const client = useQueryClient();
  const [tab, setTab] = useState<Tab>('overview'),
    [search, setSearch] = useState(''),
    [status, setStatus] = useState(''),
    [channel, setChannel] = useState(''),
    [visibility, setVisibility] = useState(''),
    [action, setAction] = useState(''),
    [page, setPage] = useState(1),
    [advanced, setAdvanced] = useState(false),
    [from, setFrom] = useState(''),
    [to, setTo] = useState(''),
    [minimum, setMinimum] = useState(''),
    [maximum, setMaximum] = useState(''),
    [sort, setSort] = useState<Sort>({ key: 'created_at', direction: 'desc' }),
    [selection, setSelection] = useState<Set<string>>(new Set()),
    [orderID, setOrderID] = useState<string | null>(null),
    [userID, setUserID] = useState<string | null>(null),
    [operation, setOperation] = useState<Operation | null>(null),
    [busy, setBusy] = useState(false),
    [banTarget, setBanTarget] = useState<User | null>(null);
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
    page_size: 20,
    action,
  };
  const endpoint =
    tab === 'orders' ? 'admin/order/list' : tab === 'users' ? 'admin/user/list' : 'admin/audit/list';
  const list = useQuery({
    queryKey: ['admin-list', tab, filters],
    queryFn: () => api<Page<Order | User | Audit>>(`${endpoint}?${query(filters)}`),
    enabled: !!profile?.admin && tab !== 'overview',
    retry: false,
  });
  const overview = useQuery({
    queryKey: ['overview'],
    queryFn: () => api<Overview>('admin/overview'),
    enabled: !!profile?.admin && tab === 'overview',
  });
  const op = useQuery({
    queryKey: ['operation', operation?.id],
    queryFn: () => api<Operation>(`admin/operation/detail?id=${operation?.id}`),
    enabled: !!operation?.confirmed,
    refetchInterval: (q) =>
      q.state.data?.items.some((i) => i.status === 'ready' || i.status === 'running') ? 2000 : false,
  });
  const currentOp = op.data ?? operation;
  // biome-ignore lint/correctness/useExhaustiveDependencies: filter changes reset selection and pagination.
  useEffect(() => {
    setPage(1);
    setSelection(new Set());
  }, [search, status, channel, visibility, from, to, minimum, maximum, action]);
  const navigate = (next: Tab) => {
    setTab(next);
    setSearch('');
    setStatus('');
    setChannel('');
    setVisibility('');
    setAction('');
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
    setOperation(await api<Operation>('admin/order/action/preview', { action, ids }));
  };
  const execute = async () => {
    if (!operation) return;
    const result = await api<Operation>('admin/order/action/execute', { operation_id: operation.id });
    setOperation(result);
    await client.invalidateQueries();
    setSelection(new Set());
    if (result.action === 'export') await downloadExport(result.id);
  };
  const rows = list.data?.items ?? [];
  function th(key: string, label: string) {
    const active = sort.key === key;
    return (
      <th aria-sort={active ? (sort.direction === 'asc' ? 'ascending' : 'descending') : undefined}>
        <button
          type="button"
          className="a-btn a-sort"
          onClick={() => {
            setSort({ key, direction: active && sort.direction === 'desc' ? 'asc' : 'desc' });
            setPage(1);
          }}
        >
          {t(label)}
          {active ? sort.direction === 'asc' ? <ArrowUp /> : <ArrowDown /> : <ArrowDownUp />}
        </button>
      </th>
    );
  }
  const checkbox = (id: string) => selection.has(id);
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
  if (app.loading)
    return (
      <main className="a-main">
        <Empty text={t('loading')} />
      </main>
    );
  if (!profile?.admin)
    return (
      <main className="a-main">
        <Empty text={profile ? t('forbidden') : t('unauthorized')} />
        {!profile && <Button onClick={() => app.setLoginOpen(true)}>{t('login')}</Button>}
        <Link to="/" className="d-btn d-ghost">
          {t('home')}
        </Link>
      </main>
    );
  return (
    <div className="a-layout">
      <nav className="a-side" aria-label={t('admin')}>
        {(
          [
            ['overview', ChartNoAxesColumn],
            ['orders', ReceiptText],
            ['users', Users],
            ['audit', ListChecks],
          ] as const
        ).map(([key, Icon]) => (
          <Button
            key={key}
            className="a-btn a-ghost"
            aria-current={tab === key ? 'page' : undefined}
            onClick={() => navigate(key)}
          >
            <Icon />
            {t(key)}
          </Button>
        ))}
        <Link className="d-btn d-ghost a-btn" to="/">
          <ArrowLeft />
          {t('home')}
        </Link>
      </nav>
      <main className="a-main">
        <div className="a-heading">
          <h1>{t(tab)}</h1>
          <div className="a-heading-actions">
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
                    <Button className="a-batch-trigger" disabled={busy}>
                      <ListChecks />
                      {t('batch')}
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
          </div>
        </div>
        {tab === 'overview' ? (
          overview.error ? (
            <Empty text={t('unavailable')} />
          ) : overview.data ? (
            <OverviewPanel data={overview.data} />
          ) : (
            <Empty text={t('loading')} />
          )
        ) : (
          <>
            <div className="a-tools">
              {tab !== 'audit' ? (
                <label className="a-search">
                  <Search />
                  <input
                    aria-label={t(tab === 'users' ? 'userSearch' : 'search')}
                    placeholder={t(tab === 'users' ? 'userSearch' : 'search')}
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
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
              )}{' '}
              {tab !== 'audit' && (
                <SelectField label={t('status')} value={status} onValue={setStatus} options={statusOptions} />
              )}{' '}
              {tab === 'orders' && (
                <>
                  <SelectField
                    label={t('channel')}
                    value={channel}
                    onValue={setChannel}
                    options={[
                      ['', t('allChannels')],
                      ['alipay', t('alipay')],
                      ['wxpay', t('wxpay')],
                    ]}
                  />
                  <Button className="a-ghost" aria-expanded={advanced} onClick={() => setAdvanced((v) => !v)}>
                    <SlidersHorizontal />
                    {t('filters')}
                  </Button>
                </>
              )}
            </div>
            <AnimatePresence initial={false}>
              {tab === 'orders' && advanced && (
                <motion.div
                  className="a-filter-wrap"
                  initial={{ height: 0, opacity: 0 }}
                  animate={{ height: 'auto', opacity: 1 }}
                  exit={{ height: 0, opacity: 0 }}
                >
                  <div className="a-filters">
                    {[
                      [from, setFrom, 'from'],
                      [to, setTo, 'to'],
                    ].map(([v, set, key]) => (
                      <div className="a-field" key={key as string}>
                        <span>{t(key as string)}</span>
                        <DateTimeField
                          value={v as string}
                          onValue={set as (v: string) => void}
                          label={t(key as string)}
                        />
                      </div>
                    ))}
                    {[
                      [minimum, setMinimum, 'minimum'],
                      [maximum, setMaximum, 'maximum'],
                    ].map(([v, set, key]) => (
                      <label className="a-field" key={key as string} htmlFor={`filter-${key}`}>
                        <span>{t(key as string)}</span>
                        <ConstrainedInput
                          id={`filter-${key}`}
                          className="a-input"
                          aria-label={t(key as string)}
                          inputMode="decimal"
                          value={v as string}
                          onValue={set as (v: string) => void}
                          accept={acceptAmountEdit}
                        />
                      </label>
                    ))}
                    <div className="a-filter-bottom">
                      <SelectField
                        label={t('visibility')}
                        value={visibility}
                        onValue={setVisibility}
                        options={['', 'public', 'hidden_order', 'hidden_message'].map((k) => [
                          k,
                          t(k || 'allVisibility'),
                        ])}
                      />
                      <Button
                        className="d-ghost"
                        onClick={() => {
                          setFrom('');
                          setTo('');
                          setMinimum('');
                          setMaximum('');
                          setVisibility('');
                        }}
                      >
                        {t('reset')}
                      </Button>
                    </div>
                    {from && to && new Date(from) >= new Date(to) && <p role="alert">{t('dateRule')}</p>}
                  </div>
                </motion.div>
              )}
            </AnimatePresence>
            {list.isLoading ? (
              <Empty text={t('loading')} />
            ) : list.error ? (
              <Empty text={t('error')} />
            ) : !rows.length ? (
              <Empty />
            ) : (
              <div className="table-scroll">
                <table className={`a-table a-${tab}`}>
                  <thead>
                    <tr>
                      {tab === 'orders' && (
                        <th className="checkbox-col">
                          <input
                            className="custom-check"
                            type="checkbox"
                            aria-label={t('selected')}
                            checked={rows.length > 0 && rows.every((r) => checkbox(r.id))}
                            onChange={() =>
                              setSelection((old) => {
                                const next = new Set(old);
                                if (rows.every((r) => next.has(r.id)))
                                  for (const r of rows) next.delete(r.id);
                                else for (const r of rows) if (next.size < 100) next.add(r.id);
                                return next;
                              })
                            }
                          />
                        </th>
                      )}
                      {th('created_at', 'time')}
                      {tab === 'orders' ? (
                        <>
                          {th('nickname', 'user')}
                          {th('amount_fen', 'amount')}
                          {th('channel', 'channel')}
                          {th('status', 'status')}
                          {th('hidden_order', 'orderVisible')}
                          {th('hidden_message', 'messageVisible')}
                        </>
                      ) : tab === 'users' ? (
                        <>
                          {th('nickname', 'user')}
                          {th('status', 'status')}
                          {th('bindings', 'bindings')}
                          {th('order_count', 'orders')}
                          {th('paid_fen', 'totalPaid')}
                          {th('last_login_at', 'lastLogin')}
                        </>
                      ) : (
                        <>
                          {th('actor', 'actor')}
                          {th('action', 'action')}
                          {th('target', 'target')}
                          {th('result', 'result')}
                        </>
                      )}
                      <th>
                        <span className="d-sr">{t('details')}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((row) => (
                      <tr key={row.id} className={selection.has(row.id) ? 'a-selected' : ''}>
                        {tab === 'orders' && (
                          <td>
                            <input
                              className="custom-check"
                              type="checkbox"
                              aria-label={`${t('selected')} ${row.id}`}
                              checked={selection.has(row.id)}
                              onChange={() => toggle(row.id)}
                            />
                          </td>
                        )}
                        <td>
                          <time>{dateTime(row.created_at)}</time>
                          <button
                            type="button"
                            className="a-sub text-button"
                            onClick={() =>
                              void navigator.clipboard.writeText(row.id).then(() => notify(t('copied')))
                            }
                          >
                            {row.id.slice(0, 8)}
                          </button>
                        </td>
                        {tab === 'orders' ? (
                          <OrderCells row={row as Order} />
                        ) : tab === 'users' ? (
                          <UserCells row={row as User} />
                        ) : (
                          <AuditCells
                            row={row as Audit}
                            openTarget={(kind, id) => (kind === 'order' ? setOrderID(id) : setUserID(id))}
                          />
                        )}
                        <td>
                          <Button
                            className="a-icon d-ghost"
                            aria-label={`${t(tab === 'users' ? 'userDetail' : 'details')} ${row.id.slice(0, 8)}`}
                            onClick={() =>
                              tab === 'orders'
                                ? setOrderID(row.id)
                                : tab === 'users'
                                  ? setUserID(row.id)
                                  : (row as Audit).target_type === 'order'
                                    ? setOrderID((row as Audit).target_id)
                                    : setUserID((row as Audit).target_id)
                            }
                          >
                            <Ellipsis />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {list.data && <Pagination page={page} size={20} total={list.data.total} onPage={setPage} />}
          </>
        )}
      </main>
      <OrderDialog id={orderID} onClose={() => setOrderID(null)} admin />
      <UserDialog id={userID} onClose={() => setUserID(null)} onOrder={setOrderID} onBan={setBanTarget} />
      <Modal
        open={!!currentOp}
        onOpenChange={(v) => {
          if (!v) {
            setOperation(null);
            client.removeQueries({ queryKey: ['operation'] });
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
            onClick={() => {
              setOperation(null);
              client.removeQueries({ queryKey: ['operation'] });
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
      <Modal
        open={!!banTarget}
        onOpenChange={(v) => !v && setBanTarget(null)}
        title={t(banTarget?.status === 'banned' ? 'unban' : 'ban')}
      >
        <p>{t(banTarget?.status === 'banned' ? 'unbanHint' : 'banHint')}</p>
        <div className="dialog-actions">
          <Button onClick={() => setBanTarget(null)}>{t('cancel')}</Button>
          <Button
            className="d-primary"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                if (!banTarget) return;
                await api(`admin/user/${banTarget.status === 'banned' ? 'unban' : 'ban'}`, {
                  id: banTarget.id,
                });
                setBanTarget(null);
                notify(t('success'));
                await client.invalidateQueries();
              })
            }
          >
            {t('confirm')}
          </Button>
        </div>
      </Modal>
    </div>
  );
}
function OrderCells({ row: o }: { row: Order }) {
  const { t } = useApp();
  return (
    <>
      <td>{o.nickname}</td>
      <td>¥{money(o.amount_fen)}</td>
      <td>{t(o.channel)}</td>
      <td>
        <span className={`a-badge ${o.status === 'paid' ? 'a-paid' : 'a-muted'}`}>{t(o.status)}</span>
      </td>
      <td>{t(o.hidden_order ? 'yes' : 'no')}</td>
      <td>{t(o.hidden_message ? 'yes' : 'no')}</td>
    </>
  );
}
function UserCells({ row: u }: { row: User }) {
  const { t } = useApp();
  return (
    <>
      <td>{u.nickname}</td>
      <td>
        <span className="a-badge">{t(u.status)}</span>
      </td>
      <td>{u.bindings.map((v) => v.provider.toUpperCase()).join(' · ')}</td>
      <td>{u.order_count}</td>
      <td>¥{money(u.paid_fen)}</td>
      <td>{dateTime(u.last_login_at)}</td>
    </>
  );
}
function AuditCells({ row: a, openTarget }: { row: Audit; openTarget: (kind: string, id: string) => void }) {
  const { t } = useApp();
  return (
    <>
      <td>{a.actor}</td>
      <td>{a.action}</td>
      <td>
        <button type="button" className="text-button" onClick={() => openTarget(a.target_type, a.target_id)}>
          {a.target}
          <small className="a-sub">{a.target_id.slice(0, 8)}</small>
        </button>
      </td>
      <td>{t(a.result)}</td>
    </>
  );
}
function OverviewPanel({ data }: { data: Overview }) {
  const { t } = useApp();
  return (
    <>
      <p className="a-muted a-small">{t('scope')}</p>
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
  const [sort, setSort] = useState<{ key: keyof Metric; asc: boolean }>({ key: 'label', asc: false });
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
      <table className="a-table">
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
                  onClick={() => setSort({ key, asc: sort.key === key ? !sort.asc : false })}
                >
                  {t(label)}
                  {sort.key === key ? sort.asc ? <ArrowUp /> : <ArrowDown /> : <ArrowDownUp />}
                </button>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map((m) => (
            <tr key={m.label}>
              <td>{title === 'channels' ? t(m.label) : m.label}</td>
              <td>¥{money(m.paid_fen)}</td>
              <td>{m.count}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {!metrics.length && <Empty />}
    </section>
  );
}
function UserDialog({
  id,
  onClose,
  onOrder,
  onBan,
}: {
  id: string | null;
  onClose: () => void;
  onOrder: (id: string) => void;
  onBan: (u: User) => void;
}) {
  const { t, profile } = useApp();
  const user = useQuery({
    queryKey: ['admin-user', id],
    queryFn: () => api<User>(`admin/user/detail?id=${id}`),
    enabled: !!id,
    retry: false,
  });
  const [page, setPage] = useState(1);
  const history = useQuery({
    queryKey: ['admin-user-orders', id, page],
    queryFn: () => api<Page<Order>>(`admin/order/list?${query({ user_id: id ?? '', page, page_size: 10 })}`),
    enabled: !!id,
    retry: false,
  });
  const u = user.data;
  // biome-ignore lint/correctness/useExhaustiveDependencies: a different user starts on their first history page.
  useEffect(() => setPage(1), [id]);
  return (
    <Modal open={!!id} onOpenChange={(v) => !v && onClose()} title={t('userDetail')} wide>
      {u ? (
        <>
          <div className="d-person">
            <Avatar name={u.nickname} src={u.avatar_url} />
            <div>
              <strong>{u.nickname}</strong>
              <span className="d-account-id">{u.id}</span>
            </div>
          </div>
          <div className="user-properties">
            {[
              [t('status'), t(u.status)],
              [t('role'), t(u.admin ? 'adminRole' : 'member')],
              [t('bindings'), u.bindings.map((v) => v.provider.toUpperCase()).join(' · ')],
            ].map(([label, value]) => (
              <div key={label}>
                <span>{label}</span>
                <strong>{value}</strong>
              </div>
            ))}
          </div>
          <div className="d-bindings">
            {u.bindings.map((b) => (
              <div key={b.id} className="d-bind">
                <Avatar name={b.nickname} src={b.avatar_url} />
                <div>
                  <p>{b.provider.toUpperCase()}</p>
                  <span className="d-secondary">{b.nickname}</span>
                </div>
              </div>
            ))}
          </div>
          <Button
            disabled={u.id === profile?.id && u.status !== 'banned'}
            onClick={() => onBan(u)}
            className={u.status === 'banned' ? '' : 'is-danger'}
          >
            {t(u.status === 'banned' ? 'unban' : 'ban')}
          </Button>
          <section className="user-history">
            <h3>{t('history')}</h3>
            {history.data?.items.map((o) => (
              <button
                className="history-row text-button"
                type="button"
                key={o.id}
                onClick={() => onOrder(o.id)}
              >
                <span>
                  {dateTime(o.created_at)}
                  <small>{t(o.status)}</small>
                </span>
                <strong>¥{money(o.amount_fen)}</strong>
              </button>
            ))}
            {history.data && <Pagination page={page} total={history.data.total} size={10} onPage={setPage} />}
          </section>
        </>
      ) : (
        <Empty text={user.isLoading ? t('loading') : t('not_found')} />
      )}
    </Modal>
  );
}
