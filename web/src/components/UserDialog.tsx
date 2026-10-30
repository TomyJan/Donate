import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useApp } from '../app/context';
import { api, dateTime, money, query } from '../lib/api';
import type { Order, Page, User } from '../lib/types';
import { ActionConfirm } from './ActionConfirm';
import { CopyButton } from './CopyButton';
import { IdentityBindings } from './IdentityBindings';
import { Avatar, Button, Empty, Modal, Pagination } from './ui';

export function UserDialog({
  id,
  onClose,
  onOrder,
  onViewOrders,
}: {
  id: string | null;
  onClose: () => void;
  onOrder: (id: string) => void;
  onViewOrders: (id: string) => void;
}) {
  const { t, profile, notify } = useApp();
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(10);
  const user = useQuery({
    queryKey: ['admin-user', id],
    queryFn: () => api<User>(`admin/user/detail?${query({ id: id ?? '' })}`),
    enabled: !!id,
    retry: false,
  });
  const history = useQuery({
    queryKey: ['admin-user-orders', id, page, size],
    queryFn: () =>
      api<Page<Order>>(`admin/order/list?${query({ user_id: id ?? '', page, page_size: size })}`),
    enabled: !!id,
    retry: false,
  });
  // biome-ignore lint/correctness/useExhaustiveDependencies: another user's history starts from its first page.
  useEffect(() => setPage(1), [id]);
  useEffect(() => {
    if (history.data)
      setPage((current) => Math.min(current, Math.max(1, Math.ceil(history.data.total / size))));
  }, [history.data, size]);
  const u = user.data;
  return (
    <Modal open={!!id} onOpenChange={(open) => !open && onClose()} title={t('userDetail')} wide>
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
              [t('lastLogin'), dateTime(u.last_login_at)],
            ].map(([label, value]) => (
              <div key={label}>
                <span>{label}</span>
                <strong>{value}</strong>
              </div>
            ))}
          </div>
          <IdentityBindings bindings={u.bindings} />
          <section className="user-history">
            <h3>{t('history')}</h3>
            {history.isLoading ? (
              <Empty text={t('loading')} />
            ) : history.error ? (
              <Empty text={t('unavailable')} />
            ) : !history.data?.items.length ? (
              <Empty />
            ) : (
              history.data.items.map((o) => (
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
              ))
            )}
            {history.data && (
              <Pagination
                page={page}
                total={history.data.total}
                size={size}
                onPage={setPage}
                onSize={setSize}
              />
            )}
          </section>
          <div className="dialog-actions user-actions">
            <CopyButton text={u.id} label={t('copy')} />
            <Button
              onClick={() => {
                onViewOrders(u.id);
                onClose();
              }}
            >
              {t('viewDonations')}
            </Button>
            <ActionConfirm
              scopeKey={u.id + u.status}
              title={t(u.status === 'banned' ? 'unban' : 'ban')}
              description={t(u.status === 'banned' ? 'unbanHint' : 'banHint')}
              danger={u.status !== 'banned'}
              trigger={
                <Button
                  className={u.status === 'banned' ? '' : 'is-danger'}
                  disabled={u.id === profile?.id && u.status !== 'banned'}
                >
                  {t(u.status === 'banned' ? 'unban' : 'ban')}
                </Button>
              }
              onConfirm={async () => {
                await api(`admin/user/${u.status === 'banned' ? 'unban' : 'ban'}`, { id: u.id });
                notify(t('success'));
                await client.invalidateQueries();
              }}
            />
          </div>
        </>
      ) : (
        <Empty text={user.isLoading ? t('loading') : t('not_found')} />
      )}
    </Modal>
  );
}
