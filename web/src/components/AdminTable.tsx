import { ArrowDown, ArrowDownUp, ArrowUp, Ellipsis } from 'lucide-react';
import type { ReactNode } from 'react';
import { useApp } from '../app/context';
import { dateTime, money } from '../lib/api';
import type { Audit, Order, User } from '../lib/types';
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
    ['hidden_order', 'orderVisible'],
    ['hidden_message', 'messageVisible'],
  ],
  users: [
    ['nickname', 'user'],
    ['status', 'status'],
    ['bindings', 'bindings'],
    ['order_count', 'orders'],
    ['paid_fen', 'totalPaid'],
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
  const { t, notify, report } = useApp();
  const headers: [string, string][] = [['created_at', 'time'], ...columns[kind]];
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
        cell('paid_fen', 'totalPaid', `¥${money(u.paid_fen)}`),
        cell('last_login_at', 'lastLogin', dateTime(u.last_login_at)),
      ];
    }
    const a = row as Audit;
    return [
      cell('actor', 'actor', a.actor),
      cell('action', 'action', a.action),
      cell(
        'target',
        'target',
        <button type="button" className="text-button" onClick={() => onTarget(a.target_type, a.target_id)}>
          {a.target}
          <small className="a-sub">{a.target_id.slice(0, 8)}</small>
        </button>,
      ),
      cell('result', 'result', t(a.result)),
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
            <th data-field="details">
              <span className="d-sr">{t('details')}</span>
            </th>
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
              <td data-field="created_at">
                <time>{dateTime(row.created_at)}</time>
                <button
                  type="button"
                  className="a-sub text-button"
                  aria-label={`${t('copy')} ${row.id}`}
                  onClick={() =>
                    void navigator.clipboard
                      .writeText(row.id)
                      .then(() => notify(t('copied')))
                      .catch(report)
                  }
                >
                  {row.id.slice(0, 8)}
                </button>
              </td>
              {fields(row)}
              <td data-field="details">
                <Button
                  className="a-icon d-ghost"
                  aria-label={`${t(kind === 'users' ? 'userDetail' : 'details')} ${row.id.slice(0, 8)}`}
                  onClick={() => onDetails(row)}
                >
                  <Ellipsis />
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
