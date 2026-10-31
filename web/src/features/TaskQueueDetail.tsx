import { useQuery, useQueryClient } from '@tanstack/react-query';
import { RotateCcw } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { ActionConfirm } from '../components/ActionConfirm';
import { AdminLoading } from '../components/AdminLoading';
import { RequestError } from '../components/RequestError';
import { Button, Empty, Modal, Pagination } from '../components/ui';
import { APIError, api, dateTime, query } from '../lib/api';
import type { Page, TaskDetail, TaskEvent } from '../lib/types';
import { taskLabel, taskTarget } from './TaskQueue';

export function TaskQueueDetail({
  id,
  scope,
  onClose,
  onOrder,
  onRetry,
  retryAllowed,
}: {
  id: string | null;
  scope: string;
  onClose: () => void;
  onOrder: (id: string) => void;
  onRetry: (ids: string[], key: string) => Promise<void>;
  retryAllowed: boolean;
}) {
  return (
    <Modal
      open={!!id && !!scope}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={useApp().t('taskDetails')}
      wide
    >
      {id && scope && (
        <TaskDetailContent
          key={`${scope}:${id}`}
          id={id}
          scope={scope}
          onClose={onClose}
          onOrder={onOrder}
          onRetry={onRetry}
          retryAllowed={retryAllowed}
        />
      )}
    </Modal>
  );
}
function TaskDetailContent({
  id,
  scope,
  onClose,
  onOrder,
  onRetry,
  retryAllowed,
}: {
  id: string;
  scope: string;
  onClose: () => void;
  onOrder: (id: string) => void;
  onRetry: (ids: string[], key: string) => Promise<void>;
  retryAllowed: boolean;
}) {
  const { t, profile } = useApp();
  const client = useQueryClient();
  const current = useRef(scope);
  current.current = profile?.admin ? `${profile.id}:${profile.csrf_token}` : '';
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(10);
  const detail = useQuery({
    queryKey: ['admin-tasks', scope, 'detail', id],
    queryFn: ({ signal }) =>
      api<TaskDetail>(
        `admin/task/detail?${query({ id })}`,
        undefined,
        signal,
        () => current.current === scope,
      ),
    enabled: !!scope && current.current === scope,
    retry: false,
  });
  const history = useQuery({
    queryKey: ['admin-tasks', scope, 'history', id, page, size],
    queryFn: ({ signal }) =>
      api<Page<TaskEvent>>(
        `admin/task/history?${query({ id, page, page_size: size })}`,
        undefined,
        signal,
        () => current.current === scope,
      ),
    enabled: !!scope && current.current === scope,
    retry: false,
  });
  const missing = [detail.error, history.error].some(
    (error) => error instanceof APIError && error.code === 'NOT_FOUND',
  );
  useEffect(() => {
    if (!missing) return;
    // A deleted order also removes its tasks. Revoke every cached history page
    // and cancel in-flight responses before the dialog unmounts.
    for (const kind of ['detail', 'history']) {
      const queryKey = ['admin-tasks', scope, kind, id];
      void client.cancelQueries({ queryKey });
      client.removeQueries({ queryKey });
    }
    onClose();
  }, [missing, client, scope, id, onClose]);
  if (missing) return null;
  const task = detail.data;
  return (
    <div className="task-detail">
      {detail.error && (
        <RequestError error={detail.error} busy={detail.isFetching} onRetry={() => void detail.refetch()} />
      )}
      {!task ? (
        !detail.error && <AdminLoading />
      ) : (
        <>
          <div className="task-detail-title">
            <div>
              <span className={`a-badge task-${task.status}`}>{taskLabel(t, 'status', task.status)}</span>
              <h3>{taskLabel(t, 'kind', task.kind)}</h3>
            </div>
            {task.retryable && (
              <ActionConfirm
                scopeKey={`${scope}:${id}`}
                title={t(task.status === 'ready' ? 'taskRunNow' : 'taskRetry')}
                prepare={async () => crypto.randomUUID()}
                onConfirm={(key) => onRetry([id], key)}
                trigger={
                  <Button disabled={!retryAllowed || !!detail.error || !!history.error} className="d-ghost">
                    <RotateCcw />
                    {t(task.status === 'ready' ? 'taskRunNow' : 'taskRetryAction')}
                  </Button>
                }
              />
            )}
          </div>
          <dl className="task-detail-grid">
            <div>
              <dt>{t('target')}</dt>
              <dd>
                <button
                  type="button"
                  className="text-button task-target"
                  onClick={() => onOrder(task.order_id)}
                >
                  {task.nickname || t('orderTarget')}
                  <small>{taskTarget(task)}</small>
                </button>
              </dd>
            </div>
            {[
              ['taskID', task.id],
              ['taskOrderID', task.order_id],
              ['taskSource', taskLabel(t, 'source', task.source)],
              ['result', taskLabel(t, 'result', task.result)],
              ['taskAttempts', String(task.total_attempts)],
              ['taskRound', String(task.attempts)],
              ['taskCreatedAt', dateTime(task.created_at)],
              ['taskUpdatedAt', dateTime(task.updated_at)],
              ['taskRunAt', dateTime(task.run_at)],
              [
                'taskLeaseUntil',
                task.status === 'running' && task.lease_until && Date.parse(task.lease_until) <= Date.now()
                  ? `${t('taskStale')} · ${dateTime(task.lease_until)}`
                  : dateTime(task.lease_until),
              ],
              ['actor', task.actor_name || t('systemActor')],
              ['taskRequestID', task.request_id || '—'],
            ].map(([key, value]) => (
              <div key={key}>
                <dt>{t(key)}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
          <p className="task-history-since">
            {t('taskRecordingSince')} {dateTime(task.recording_since)}
          </p>
        </>
      )}
      <section className="task-history">
        <h3>{t('taskHistory')}</h3>
        {history.error && (
          <RequestError
            error={history.error}
            busy={history.isFetching}
            onRetry={() => void history.refetch()}
          />
        )}
        {history.isLoading ? (
          <AdminLoading kind="table" />
        ) : history.data?.items.length ? (
          <ol className="task-timeline">
            {history.data.items.map((event) => (
              <li key={event.id}>
                <div className="task-event-heading">
                  <strong>{taskLabel(t, 'event', event.event)}</strong>
                  <time>{dateTime(event.created_at)}</time>
                </div>
                <div className="task-event-meta">
                  <span>{taskLabel(t, 'status', event.status)}</span>
                  <span>{taskLabel(t, 'source', event.source)}</span>
                  <span>{t('taskAttemptNumber').replace('{count}', String(event.attempt))}</span>
                  {event.duration_ms !== undefined && <span>{event.duration_ms} ms</span>}
                </div>
                {event.result && <p>{taskLabel(t, 'result', event.result)}</p>}
                {event.message && <p className="task-event-message">{event.message}</p>}
                {event.run_at && (
                  <small>
                    {t('taskRunAt')} · {dateTime(event.run_at)}
                  </small>
                )}
                {(event.actor_name || event.request_id) && (
                  <small>
                    {event.actor_name}
                    {event.actor_name && event.request_id && ' · '}
                    {event.request_id}
                  </small>
                )}
              </li>
            ))}
          </ol>
        ) : (
          !history.error && <Empty />
        )}
        {history.data && (
          <Pagination
            page={page}
            size={size}
            total={history.data.total}
            onPage={setPage}
            onSize={(value) => {
              setSize(value);
              setPage(1);
            }}
          />
        )}
      </section>
    </div>
  );
}
