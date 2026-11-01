import { ArrowDown, ArrowDownUp, ArrowUp, Ellipsis } from 'lucide-react';
import type { ReactNode } from 'react';
import { useApp } from '../app/context';
import { dateTime, money } from '../lib/api';
import type { Audit, Order, User } from '../lib/types';
import { MessagePreview } from './MessagePreview';
import { Button } from './ui';

type Kind = 'orders' | 'users' | 'audit';
type Row = Order | User | Audit;
type Sort = { key: string; direction: 'asc' | 'desc' };
type Props = {
  kind: Kind;
  rows: Row[];
  sort: Sort;
  onSort: (sort: Sort) => void;
  selection: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: () => void;
  onDetails: (row: Row) => void;
  onTarget: (kind: string, id: string) => void;
};
const columns: Record<Kind, [string, string][]> = {
  orders: [
    ['nickname', 'user'],
    ['amount_fen', 'amount'],
    ['channel', 'channel'],
    ['status', 'status'],
    ['message', 'messageColumn'],
    ['hidden_order', 'orderVisible'],
    ['hidden_message', 'messageVisible'],
  ],
  users: [
    ['nickname', 'user'],
    ['status', 'status'],
    ['bindings', 'bindings'],
    ['order_count', 'orders'],
    ['paid_fen', 'donationAmount'],
    ['last_login_at', 'lastLogin'],
  ],
  audit: [
    ['actor', 'actor'],
    ['action', 'action'],
    ['target', 'target'],
    ['result', 'result'],
  ],
};

export function AdminTable({
  kind,
  rows,
  sort,
  onSort,
  selection,
  onToggle,
  onToggleAll,
  onDetails,
  onTarget,
}: Props) {
  const { t } = useApp();
  const timeLabel = kind === 'users' ? 'registeredAt' : 'time';
  const headers: [string, string][] = [['created_at', timeLabel], ...columns[kind]];
  const cell = (field: string, label: string, content: ReactNode) => (
    <td key={field} data-field={field} data-label={t(label)}>
      {content}
    </td>
  );
  function fields(row: Row) {
    if (kind === 'orders') {
      const o = row as Order;
      return [
        cell('nickname', 'user', o.nickname || '—'),
        cell('amount_fen', 'amount', `¥${money(o.amount_fen)}`),
        cell('channel', 'channel', t(o.channel)),
        cell(
          'status',
          'status',
          <span className={`a-badge ${o.status === 'paid' ? 'a-paid' : 'a-muted'}`}>{t(o.status)}</span>,
        ),
        cell('message', 'messageColumn', <MessagePreview message={o.message} />),
        cell('hidden_order', 'orderVisible', t(o.hidden_order ? 'yes' : 'no')),
        cell('hidden_message', 'messageVisible', t(o.hidden_message ? 'yes' : 'no')),
      ];
    }
    if (kind === 'users') {
      const u = row as User;
      return [
        cell('nickname', 'user', u.nickname || '—'),
        cell('status', 'status', <span className="a-badge">{t(u.status)}</span>),
        cell('bindings', 'bindings', u.bindings.map((v) => v.provider.toUpperCase()).join(' · ')),
        cell('order_count', 'orders', u.order_count),
        cell('paid_fen', 'donationAmount', `¥${money(u.paid_fen)}`),
        cell('last_login_at', 'lastLogin', dateTime(u.last_login_at)),
      ];
    }
    const a = row as Audit;
    return [
      cell('actor', 'actor', a.actor === 'system' ? t('systemActor') : a.actor),
      cell('action', 'action', t(`audit_${a.action.replaceAll('.', '_')}`)),
      cell(
        'target',
        'target',
        a.target_type === 'task' ? (
          <span className="audit-target">
            {a.target_exists
              ? `${t('taskTarget')} · ${t(`task_resource_${a.target_resource}`)}`
              : t('deletedTask')}
          </span>
        ) : (
          <button
            type="button"
            className="text-button audit-target"
            disabled={!a.target_exists}
            onClick={() => onTarget(a.target_type, a.target_id)}
          >
            {a.target_exists ? (
              <>
                <span>
                  {a.target_nickname || t(a.target_type === 'order' ? 'orderTarget' : 'userTarget')}
                </span>
                {a.target_type === 'order' && a.target_amount_fen != null && (
                  <small>¥{money(a.target_amount_fen)}</small>
                )}
              </>
            ) : (
              t(a.target_type === 'order' ? 'deletedOrder' : 'deletedUser')
            )}
          </button>
        ),
      ),
      cell(
        'result',
        'result',
        t(a.result === 'created' ? 'createdResult' : a.result === 'unbound' ? 'unboundResult' : a.result),
      ),
    ];
  }
  return (
    <section className="table-scroll" aria-label={t(kind)}>
      <table className={`a-table a-data-table a-${kind}`}>
        <thead>
          <tr>
            {kind === 'orders' && (
              <th data-field="select">
                <input
                  className="custom-check"
                  type="checkbox"
                  aria-label={t('selected')}
                  checked={rows.every((row) => selection.has(row.id))}
                  onChange={onToggleAll}
                />
              </th>
            )}
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
                  onClick={() =>
                    onSort({ key, direction: sort.key === key && sort.direction === 'desc' ? 'asc' : 'desc' })
                  }
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
            {kind !== 'audit' && (
              <th data-field="details">
                <span className="d-sr">{t('details')}</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.id} className={selection.has(row.id) ? 'a-selected' : undefined}>
              {kind === 'orders' && (
                <td data-field="select">
                  <input
                    className="custom-check"
                    type="checkbox"
                    aria-label={`${t('selected')} ${row.id}`}
                    checked={selection.has(row.id)}
                    onChange={() => onToggle(row.id)}
                  />
                </td>
              )}
              <td data-field="created_at" data-label={t(timeLabel)}>
                <time>{dateTime(row.created_at)}</time>
              </td>
              {fields(row)}
              {kind !== 'audit' && (
                <td data-field="details">
                  <Button
                    className="a-icon d-ghost"
                    aria-label={`${t(kind === 'users' ? 'userDetail' : 'details')} ${row.id.slice(0, 8)}`}
                    onClick={() => onDetails(row)}
                  >
                    <Ellipsis />
                  </Button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
