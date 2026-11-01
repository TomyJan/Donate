import * as Popover from '@radix-ui/react-popover';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Check, ChevronDown, Search } from 'lucide-react';
import { useId, useState } from 'react';
import { useApp } from '../app/context';
import { useRequestScope } from '../app/useRequestScope';
import { api, query } from '../lib/api';
import type { Page, User } from '../lib/types';
import { RequestError } from './RequestError';
import { Button, Empty, portal } from './ui';

export function AdminUserFilter({ value, onValue }: { value: string; onValue: (id: string) => void }) {
  const { t, profile } = useApp();
  const { scopeKey, isCurrent } = useRequestScope('admin-user-filter', true);
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const listId = useId();
  const selected = useQuery({
    queryKey: ['admin-user', value, scopeKey],
    queryFn: ({ signal }) =>
      api<User>(`admin/user/detail?${query({ id: value })}`, undefined, signal, isCurrent),
    enabled: !!value && !!profile?.admin,
    retry: false,
  });
  const users = useQuery({
    queryKey: ['admin-user-options', search, scopeKey],
    queryFn: ({ signal }) =>
      api<Page<User>>(
        `admin/user/list?${query({ search, page: 1, page_size: 20 })}`,
        undefined,
        signal,
        isCurrent,
      ),
    enabled: open && !!profile?.admin,
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[2] === scopeKey ? previous : undefined,
    retry: false,
  });
  const choose = (user?: User) => {
    if (!isCurrent()) return;
    if (user) client.setQueryData(['admin-user', user.id, scopeKey], user);
    onValue(user?.id ?? '');
    setOpen(false);
  };
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <Button
          className="a-select a-user-filter"
          role="combobox"
          aria-label={t('filterUser')}
          aria-expanded={open}
          aria-controls={listId}
        >
          <span>{value ? selected.data?.nickname || value.slice(0, 8) : t('allUsers')}</span>
          <ChevronDown />
        </Button>
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content className="d-pop admin-user-pop" sideOffset={8} collisionPadding={12} align="end">
          <label className="a-search admin-user-search">
            <Search />
            <input
              aria-label={t('filterUserSearch')}
              placeholder={t('userSearch')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <div
            id={listId}
            role="listbox"
            aria-label={t('filterUser')}
            className="admin-user-options"
            aria-busy={users.isFetching}
          >
            <Button
              role="option"
              aria-selected={!value}
              data-selected={!value || undefined}
              className="d-option admin-user-option"
              onClick={() => choose()}
            >
              <span>{t('allUsers')}</span>
              {!value && <Check />}
            </Button>
            {users.data?.items.map((user) => (
              <Button
                key={user.id}
                role="option"
                aria-selected={user.id === value}
                data-selected={user.id === value || undefined}
                className="d-option admin-user-option"
                onClick={() => choose(user)}
              >
                <span>
                  <strong>{user.nickname || '—'}</strong>
                  <small>{user.id}</small>
                </span>
                {user.id === value && <Check />}
              </Button>
            ))}
            {!users.data?.items.length &&
              (users.error ? (
                <RequestError
                  error={users.error}
                  busy={users.isFetching}
                  onRetry={() => void users.refetch()}
                />
              ) : (
                <Empty text={t(users.isLoading ? 'loading' : 'empty')} />
              ))}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
