import * as Popover from '@radix-ui/react-popover';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ArrowDown,
  ArrowDownUp,
  ArrowUp,
  CalendarRange,
  Ellipsis,
  RefreshCw,
  RotateCcw,
  Search,
  X,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { ActionConfirm } from '../components/ActionConfirm';
import { AdminHeading } from '../components/AdminHeading';
import { AdminLoading } from '../components/AdminLoading';
import { DialogActions } from '../components/DialogActions';
import { DateTimeField, SelectField } from '../components/forms';
import { OrderDialog } from '../components/OrderDialog';
import { type OverviewPeriod, OverviewRange } from '../components/OverviewRange';
import { RequestError } from '../components/RequestError';
import { Button, Empty, Modal, Pagination, portal } from '../components/ui';
import { api, dateTime, query } from '../lib/api';
import type { Page, TaskItem, TaskOverview, TaskRetryResult } from '../lib/types';
import { TaskQueueDetail } from './TaskQueueDetail';
import { TaskQueueTrend } from './TaskQueueTrend';
import './TaskQueue.css';

export function taskLabel(t: (key: string) => string, group: string, code: string) {
  if (!code) return '—';
  const key = `task_${group}_${code}`;
  const translated = t(key);
  return translated === key ? code : translated;
}
export function taskTarget(task: TaskItem) {
  return task.merchant_order_no || task.nickname || task.order_id.slice(0, 8);
}
export function TaskQueue() {
  const { profile, t, notify } = useApp();
  const client = useQueryClient();
  const scope = profile?.admin ? `${profile.id}:${profile.csrf_token}` : '';
  const current = useRef(scope);
  current.current = scope;
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const [visible, setVisible] = useState(!document.hidden);
  useEffect(() => {
    const changed = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', changed);
    return () => document.removeEventListener('visibilitychange', changed);
  }, []);
  const [auto, setAuto] = useState(true);
  const [period, setPeriod] = useState<OverviewPeriod>({ range: '7d' });
  const [filters, setFilters] = useState({ search: '', kind: '', status: '', source: '', from: '', to: '' });
  const [sort, setSort] = useState({ key: 'created_at', direction: 'desc' as 'asc' | 'desc' });
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [detailID, setDetailID] = useState<string | null>(null);
  const [orderID, setOrderID] = useState<string | null>(null);
  const [results, setResults] = useState<{ result: TaskRetryResult; names: Record<string, string> } | null>(
    null,
  );
  const [lastUpdated, setLastUpdated] = useState('');
  const queryRoot = ['admin-tasks', scope];
  const list = useQuery({
    queryKey: [...queryRoot, 'list', filters, sort, page, size],
    queryFn: ({ signal }) =>
      api<Page<TaskItem>>(
        'admin/task/list?' +
          query({ ...filters, sort: sort.key, direction: sort.direction, page, page_size: size }),
        undefined,
        signal,
        () => current.current === scope && alive.current,
      ),
    enabled: !!scope,
    retry: false,
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === scope ? previous : undefined,
  });
  const overview = useQuery({
    queryKey: [...queryRoot, 'overview', period],
    queryFn: ({ signal }) =>
      api<TaskOverview>(
        `admin/task/overview?${query(period)}`,
        undefined,
        signal,
        () => current.current === scope && alive.current,
      ),
    enabled: !!scope,
    retry: false,
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === scope ? previous : undefined,
  });
  const refresh = () => client.invalidateQueries({ queryKey: queryRoot });
  useEffect(() => {
    if (!scope || !auto || !visible) return;
    const timer = window.setInterval(() => {
      void client.invalidateQueries({ queryKey: ['admin-tasks', scope] });
    }, 5000);
    return () => window.clearInterval(timer);
  }, [scope, auto, visible, client]);
  useEffect(() => {
    if (list.dataUpdatedAt) setLastUpdated(new Date(list.dataUpdatedAt).toISOString());
  }, [list.dataUpdatedAt]);
  useEffect(() => {
    if (list.data && !list.isPlaceholderData)
      setPage((value) => Math.min(value, Math.max(1, Math.ceil(list.data.total / size))));
  }, [list.data, list.isPlaceholderData, size]);
  useEffect(() => {
    setSelection(new Set());
    setDetailID(null);
    setOrderID(null);
    setResults(null);
    return () => {
      void client.cancelQueries({ queryKey: ['admin-tasks', scope] });
      client.removeQueries({ queryKey: ['admin-tasks', scope] });
    };
  }, [scope, client]);
  const change = (value: Partial<typeof filters>) => {
    setFilters((old) => ({ ...old, ...value }));
    setPage(1);
    setSelection(new Set());
  };
  const rows = list.data?.items ?? [];
  const toggle = (id: string) =>
    setSelection((old) => {
      const next = new Set(old);
      if (next.has(id)) next.delete(id);
      else if (next.size < 100) next.add(id);
      return next;
    });
  const retry = async (ids: string[], key: string) => {
    if (
      !scope ||
      current.current !== scope ||
      !alive.current ||
      list.error ||
      overview.error ||
      list.isPlaceholderData
    )
      return;
    const names = Object.fromEntries(rows.map((item) => [item.id, taskTarget(item)]));
    const result = await api<TaskRetryResult>(
      'admin/task/retry',
      { ids, idempotency_key: key },
      undefined,
      () => current.current === scope && alive.current,
    );
    if (current.current !== scope || !alive.current) return;
    setResults({ result, names });
    setSelection(new Set());
    notify(t('taskRetryFinished'));
    await refresh();
  };
  const retryButton = (ids: string[], label: string, compact = false, ready = false) => (
    <ActionConfirm
      scopeKey={`${scope}:${ids.join(',')}`}
      title={t(ids.length > 1 ? 'taskRetrySelected' : ready ? 'taskRunNow' : 'taskRetry')}
      prepare={async () => crypto.randomUUID()}
      onConfirm={(key) => retry(ids, key)}
      trigger={
        <Button
          disabled={!!list.error || !!overview.error || list.isPlaceholderData}
          className={compact ? 'd-ghost a-icon' : 'a-batch-trigger'}
          aria-label={label}
          title={label}
        >
          <RotateCcw />
          {!compact && <span>{t('taskRetrySelected')}</span>}
        </Button>
      }
    />
  );
  const stats = overview.data;
  const headers = [
    ['created_at', 'taskCreatedAt'],
    ['kind', 'taskKind'],
    ['order_id', 'target'],
    ['status', 'status'],
    ['total_attempts', 'taskAttempts'],
    ['run_at', 'taskRunAt'],
    ['updated_at', 'taskUpdatedAt'],
  ];
  return (
    <div className="task-workspace">
      <AdminHeading title={t('tasks')}>
        {selection.size > 0 && (
          <>
            <span className="a-selection-count">
              {t('selected')} {selection.size}
            </span>
            <Button
              className="d-ghost a-icon"
              aria-label={t('clear')}
              onClick={() => setSelection(new Set())}
            >
              <X />
            </Button>
            {retryButton([...selection], t('taskRetrySelected'))}
          </>
        )}
        <Button
          className="d-ghost a-icon"
          aria-label={t('taskRefresh')}
          title={t('taskRefresh')}
          disabled={list.isFetching || overview.isFetching}
          onClick={() => void refresh()}
        >
          <RefreshCw />
        </Button>
      </AdminHeading>
      <div className="task-current-heading">
        <span>{t('taskCurrentQueue')}</span>
        <label className="task-auto">
          <input type="checkbox" checked={auto} onChange={(event) => setAuto(event.target.checked)} />
          {t('taskAutoRefresh')}
        </label>
      </div>
      {overview.error && (
        <RequestError
          error={overview.error}
          busy={overview.isFetching}
          onRetry={() => void overview.refetch()}
        />
      )}
      {stats ? (
        <section className="task-stats" aria-label={t('taskCurrentQueue')}>
          {(['ready', 'running', 'done', 'failed'] as const).map((status) => (
            <button
              type="button"
              className={`task-stat task-${status}`}
              key={status}
              aria-pressed={filters.status === status}
              onClick={() => change({ status: filters.status === status ? '' : status })}
            >
              <span>
                <i />
                {taskLabel(t, 'status', status)}
              </span>
              <strong>{stats.counts[status]}</strong>
              <small>
                {status === 'ready'
                  ? t('taskScheduleCounts')
                      .replace('{due}', String(stats.due))
                      .replace('{scheduled}', String(stats.scheduled))
                  : status === 'running' && stats.stale_running > 0
                    ? t('taskStaleCount').replace('{count}', String(stats.stale_running))
                    : t('taskCurrentState')}
              </small>
            </button>
          ))}
        </section>
      ) : (
        !overview.error && <AdminLoading />
      )}
      <section className="task-trend-panel">
        <div className="task-section-heading">
          <div>
            <h2>{t('taskTrend')}</h2>
            <p>{t('taskRangeHint')}</p>
          </div>
          <OverviewRange value={period} onChange={setPeriod} />
        </div>
        {stats && <TaskQueueTrend data={stats.trend} />}
      </section>
      <section className="task-list-panel" aria-label={t('taskList')}>
        <div className="a-tools task-tools">
          <label className="a-search">
            <Search />
            <input
              aria-label={t('taskSearch')}
              placeholder={t('taskSearch')}
              value={filters.search}
              onChange={(event) => change({ search: event.target.value })}
            />
          </label>
          <SelectField
            label={t('taskKind')}
            value={filters.kind}
            onValue={(kind) => change({ kind })}
            options={['', 'payment', 'geo', 'close'].map((key) => [
              key,
              key ? taskLabel(t, 'kind', key) : t('taskAllKinds'),
            ])}
          />
          <SelectField
            label={t('status')}
            value={filters.status}
            onValue={(status) => change({ status })}
            options={['', 'ready', 'running', 'done', 'failed'].map((key) => [
              key,
              key ? taskLabel(t, 'status', key) : t('allStatus'),
            ])}
          />
          <SelectField
            label={t('taskSource')}
            value={filters.source}
            onValue={(source) => change({ source })}
            options={[
              '',
              'automatic',
              'manual',
              'browser_return',
              'admin',
              'admin_retry',
              'admin_refresh',
              'close_followup',
            ].map((key) => [key, key ? taskLabel(t, 'source', key) : t('taskAllSources')])}
          />
          <TaskDates from={filters.from} to={filters.to} onChange={(from, to) => change({ from, to })} />
        </div>
        {list.error && (
          <RequestError error={list.error} busy={list.isFetching} onRetry={() => void list.refetch()} />
        )}
        {list.isLoading ? (
          <AdminLoading kind="table" />
        ) : rows.length ? (
          <section className="table-scroll task-table-wrap">
            <table className="a-table task-table">
              <thead>
                <tr>
                  <th data-field="select">
                    <input
                      className="custom-check"
                      type="checkbox"
                      aria-label={t('taskSelectAll')}
                      checked={rows.length > 0 && rows.every((row) => selection.has(row.id))}
                      onChange={() =>
                        setSelection((old) => {
                          const next = new Set(old);
                          if (rows.every((row) => next.has(row.id)))
                            for (const row of rows) next.delete(row.id);
                          else for (const row of rows) if (next.size < 100) next.add(row.id);
                          return next;
                        })
                      }
                    />
                  </th>
                  {headers.map(([key, label]) => (
                    <th
                      key={key}
                      data-field={key}
                      aria-sort={
                        sort.key === key ? (sort.direction === 'asc' ? 'ascending' : 'descending') : undefined
                      }
                    >
                      <button
                        type="button"
                        className="a-sort"
                        onClick={() => {
                          setSort({
                            key,
                            direction: sort.key === key && sort.direction === 'desc' ? 'asc' : 'desc',
                          });
                          setPage(1);
                          setSelection(new Set());
                        }}
                      >
                        {t(label)}
                        {sort.key === key ? (
                          sort.direction === 'asc' ? (
                            <ArrowUp />
                          ) : (
                            <ArrowDown />
                          )
                        ) : (
                          <ArrowDownUp />
                        )}
                      </button>
                    </th>
                  ))}
                  <th data-field="actions">
                    <span className="task-sr">{t('action')}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((task) => (
                  <tr key={task.id} className={selection.has(task.id) ? 'a-selected' : ''}>
                    <td data-field="select">
                      <input
                        className="custom-check"
                        type="checkbox"
                        aria-label={`${t('taskSelect')} ${taskTarget(task)}`}
                        checked={selection.has(task.id)}
                        onChange={() => toggle(task.id)}
                      />
                    </td>
                    <td data-field="created_at" data-label={t('taskCreatedAt')}>
                      <time>{dateTime(task.created_at)}</time>
                    </td>
                    <td data-field="kind" data-label={t('taskKind')}>
                      {taskLabel(t, 'kind', task.kind)}
                      <small className="a-sub">{taskLabel(t, 'source', task.source)}</small>
                    </td>
                    <td data-field="order_id" data-label={t('target')}>
                      <button
                        type="button"
                        className="text-button task-target"
                        onClick={() => setOrderID(task.order_id)}
                      >
                        {task.nickname || t('orderTarget')}
                        <small>{task.merchant_order_no || task.order_id.slice(0, 8)}</small>
                      </button>
                    </td>
                    <td data-field="status" data-label={t('status')}>
                      <span className={`a-badge task-${task.status}`}>
                        {taskLabel(t, 'status', task.status)}
                      </span>
                      <small className="a-sub">
                        {task.status === 'running' &&
                        task.lease_until &&
                        Date.parse(task.lease_until) <= Date.now()
                          ? t('taskStale')
                          : taskLabel(t, 'result', task.result)}
                      </small>
                    </td>
                    <td data-field="total_attempts" data-label={t('taskAttempts')}>
                      {task.total_attempts}
                      <small className="a-sub">
                        {t('taskRoundAttempts').replace('{count}', String(task.attempts))}
                      </small>
                    </td>
                    <td data-field="run_at" data-label={t('taskRunAt')}>
                      <time>{dateTime(task.run_at)}</time>
                    </td>
                    <td data-field="updated_at" data-label={t('taskUpdatedAt')}>
                      <time>{dateTime(task.updated_at)}</time>
                    </td>
                    <td data-field="actions">
                      <div className="task-row-actions">
                        {task.retryable &&
                          retryButton(
                            [task.id],
                            `${t(task.status === 'ready' ? 'taskRunNow' : 'taskRetryAction')} ${taskTarget(task)}`,
                            true,
                            task.status === 'ready',
                          )}
                        <Button
                          className="d-ghost a-icon"
                          aria-label={`${t('taskDetails')} ${taskTarget(task)}`}
                          onClick={() => setDetailID(task.id)}
                        >
                          <Ellipsis />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        ) : (
          !list.error && <Empty />
        )}
        {list.data && (
          <Pagination
            page={page}
            size={size}
            total={list.data.total}
            onPage={(value) => {
              setPage(value);
              setSelection(new Set());
            }}
            onSize={(value) => {
              setSize(value);
              setPage(1);
              setSelection(new Set());
            }}
          />
        )}
      </section>
      <p className="task-update" role="status">
        {lastUpdated ? `${t('taskUpdated')} ${dateTime(lastUpdated)}` : ''}
        {auto && ` · ${t(visible ? 'taskEveryFiveSeconds' : 'taskPollingPaused')}`}
      </p>
      <TaskQueueDetail
        id={detailID}
        scope={scope}
        onClose={() => setDetailID(null)}
        onOrder={(id) => setOrderID(id)}
        onRetry={retry}
        retryAllowed={!list.error && !overview.error && !list.isPlaceholderData}
      />
      <OrderDialog id={orderID} onClose={() => setOrderID(null)} admin />
      <Modal
        open={!!results}
        onOpenChange={(open) => {
          if (!open) setResults(null);
        }}
        title={t('result')}
      >
        <div className="operation-items task-results">
          {results?.result.items.map((item, index) => (
            <div key={item.id}>
              <span>
                {results.names[item.id] || t('taskItemNumber').replace('{number}', String(index + 1))}
              </span>
              <span>{taskLabel(t, 'result', item.result)}</span>
            </div>
          ))}
        </div>
        <DialogActions>
          <Button onClick={() => setResults(null)}>{t('close')}</Button>
        </DialogActions>
      </Modal>
    </div>
  );
}
function TaskDates({
  from,
  to,
  onChange,
}: {
  from: string;
  to: string;
  onChange: (from: string, to: string) => void;
}) {
  const { t, notify } = useApp();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState({ from, to });
  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        if (next) setDraft({ from, to });
        setOpen(next);
      }}
    >
      <Popover.Trigger asChild>
        <Button className="task-date-toggle" aria-label={t('taskCreatedRange')}>
          <CalendarRange />
          {t('taskCreatedRange')}
          {(from || to) && <i />}
        </Button>
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content
          className="d-pop range-pop"
          align="end"
          sideOffset={8}
          collisionPadding={12}
          aria-label={t('taskCreatedRange')}
        >
          {(['from', 'to'] as const).map((key) => (
            <div className="range-field" key={key}>
              <span>{t(key)}</span>
              <DateTimeField
                label={t(key)}
                value={draft[key]}
                onValue={(value) => setDraft((old) => ({ ...old, [key]: value }))}
              />
            </div>
          ))}
          <div className="task-date-actions">
            <Button
              className="d-ghost"
              onClick={() => {
                onChange('', '');
                setDraft({ from: '', to: '' });
                setOpen(false);
              }}
            >
              {t('reset')}
            </Button>
            <Button
              className="d-primary"
              onClick={() => {
                if (draft.from && draft.to && Date.parse(draft.from) >= Date.parse(draft.to)) {
                  notify(t('dateRule'), true);
                  return;
                }
                onChange(draft.from, draft.to);
                setOpen(false);
              }}
            >
              {t('apply')}
            </Button>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
