import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useApp } from '../app/context';
import { api, dateTime, query } from '../lib/api';
import type { User } from '../lib/types';
import { ActionConfirm } from './ActionConfirm';
import { CopyButton } from './CopyButton';
import { IdentityBindings } from './IdentityBindings';
import { Avatar, Button, Empty, Modal } from './ui';

export function UserDialog({
  id,
  onClose,
  onViewOrders,
}: {
  id: string | null;
  onClose: () => void;
  onViewOrders: (id: string) => void;
}) {
  const { t, profile, notify } = useApp();
  const client = useQueryClient();
  const user = useQuery({
    queryKey: ['admin-user', id],
    queryFn: () => api<User>(`admin/user/detail?${query({ id: id ?? '' })}`),
    enabled: !!id,
    retry: false,
  });
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
