import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useApp } from '../app/context';
import { useRequestScope } from '../app/useRequestScope';
import { api, dateTime, money, query } from '../lib/api';
import type { Profile, User } from '../lib/types';
import { ActionConfirm } from './ActionConfirm';
import { AdminLoading } from './AdminLoading';
import { CopyButton } from './CopyButton';
import { DialogActions } from './DialogActions';
import { IdentityBindings } from './IdentityBindings';
import { RequestError } from './RequestError';
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
  const { scopeKey, isCurrent } = useRequestScope(id, true);
  const user = useQuery({
    queryKey: ['admin-user', id, scopeKey],
    queryFn: ({ signal }) =>
      api<User>(`admin/user/detail?${query({ id: id ?? '' })}`, undefined, signal, isCurrent),
    enabled: !!id && !!profile?.admin,
    retry: false,
  });
  const u = user.data;
  const mutate = async (path: string, body: unknown) => {
    if (!isCurrent()) return false;
    try {
      await api(path, body, undefined, isCurrent);
    } catch (error) {
      if (isCurrent()) throw error;
      return false;
    }
    return isCurrent();
  };
  const unbind = async (ownerID: string, identityID: string) => {
    if (id !== ownerID) return;
    if (!(await mutate('admin/user/identity/unbind', { id: ownerID, identity_id: identityID }))) return;
    let refetchType: 'active' | 'none' = 'active';
    if (ownerID === profile?.id) {
      await client.invalidateQueries({ queryKey: ['profile'] });
      if (!isCurrent()) return;
      const current = client.getQueryState<Profile | null>(['profile']);
      if (current?.status !== 'success' || !current.data?.admin) refetchType = 'none';
    }
    await Promise.all([
      client.invalidateQueries({ queryKey: ['admin-user', ownerID], refetchType }),
      client.invalidateQueries({ queryKey: ['admin-list'], refetchType }),
      client.invalidateQueries({ queryKey: ['admin-user-options'], refetchType }),
      client.invalidateQueries({ queryKey: ['records'], refetchType }),
    ]);
    if (isCurrent()) notify(t('success'));
  };
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
              [t('donationAmount'), `¥${money(u.paid_fen)}`],
              [t('lastLogin'), dateTime(u.last_login_at)],
            ].map(([label, value]) => (
              <div key={label}>
                <span>{label}</span>
                <strong>{value}</strong>
              </div>
            ))}
          </div>
          <IdentityBindings
            key={scopeKey}
            ownerID={u.id}
            bindings={u.bindings}
            onUnbind={(identityID) => unbind(u.id, identityID)}
          />
          <DialogActions className="user-actions">
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
              scopeKey={`${scopeKey}:${u.status}`}
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
                if (!(await mutate(`admin/user/${u.status === 'banned' ? 'unban' : 'ban'}`, { id: u.id })))
                  return;
                notify(t('success'));
                await client.invalidateQueries();
              }}
            />
          </DialogActions>
        </>
      ) : user.isLoading ? (
        <AdminLoading kind="detail" />
      ) : user.error ? (
        <RequestError error={user.error} busy={user.isFetching} onRetry={() => void user.refetch()} />
      ) : (
        <Empty text={t('not_found')} />
      )}
    </Modal>
  );
}
