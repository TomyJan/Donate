import * as Popover from '@radix-ui/react-popover';
import { UserRoundPlus } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { api } from '../lib/api';
import { errorMessage } from '../lib/errors';
import type { User } from '../lib/types';
import { AdminUserFilter } from './AdminUserFilter';
import { Button, portal } from './ui';
import './OrderMigration.css';

export function OrderMigration({
  id,
  scope,
  isCurrent,
  disabled,
  onBusy,
  onMigrated,
}: {
  id: string;
  scope: object;
  isCurrent: () => boolean;
  disabled: boolean;
  onBusy: (busy: boolean) => void;
  onMigrated: (userID: string) => Promise<void>;
}) {
  const { t } = useApp();
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<User>();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const version = useRef(0);
  const working = useRef(false);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Scope changes and failed detail refreshes revoke selection and pending confirmations.
  useEffect(() => {
    version.current += 1;
    working.current = false;
    setOpen(false);
    setTarget(undefined);
    setConfirming(false);
    setBusy(false);
    setError(undefined);
    onBusy(false);
    return () => {
      version.current += 1;
      working.current = false;
      onBusy(false);
    };
  }, [scope, disabled, onBusy]);
  const changeOpen = (value: boolean) => {
    if (working.current || (value && (disabled || !isCurrent()))) return;
    setOpen(value);
    setConfirming(false);
    setError(undefined);
    if (!value) setTarget(undefined);
  };
  const migrate = async () => {
    if (!target || !confirming || disabled || working.current || !isCurrent()) return;
    const current = version.current;
    const active = () => current === version.current && isCurrent();
    working.current = true;
    setBusy(true);
    onBusy(true);
    setError(undefined);
    try {
      const result = await api<{ id: string; user_id: string; result: 'migrated' | 'unchanged' }>(
        'admin/order/migrate',
        { id, user_id: target.id },
        undefined,
        active,
      );
      if (!active()) return;
      if (
        result.id !== id ||
        result.user_id !== target.id ||
        !['migrated', 'unchanged'].includes(result.result)
      )
        throw new Error('Invalid migration result');
      await onMigrated(target.id);
    } catch (failure) {
      if (!active()) return;
      setError(failure);
      setConfirming(false);
    } finally {
      if (active()) {
        working.current = false;
        setBusy(false);
        onBusy(false);
      }
    }
  };
  return (
    <Popover.Root open={open && !disabled} onOpenChange={changeOpen}>
      <Popover.Trigger asChild>
        <Button disabled={disabled || busy}>
          <UserRoundPlus />
          {t('migrateOrder')}
        </Button>
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content
          className="order-migration action-confirm d-pop"
          sideOffset={8}
          collisionPadding={12}
          align="end"
          aria-label={t('migrateOrder')}
          aria-busy={busy}
          onEscapeKeyDown={(event) => {
            if (busy) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (busy) event.preventDefault();
          }}
        >
          <p className="action-confirm-prompt">{t(confirming ? 'migrationConfirmPrompt' : 'migrateOrder')}</p>
          <p className="order-migration-hint">{t('migrationHint')}</p>
          {!confirming && (
            <AdminUserFilter
              value={target?.id ?? ''}
              label="migrationTarget"
              emptyLabel="migrationSelectUser"
              allowEmpty={false}
              onValue={(_id, user) => {
                if (disabled || !isCurrent()) return;
                setTarget(user);
                setError(undefined);
              }}
            />
          )}
          {target && (
            <div className="order-migration-target">
              <strong>{target.nickname || '—'}</strong>
              <code>{target.id}</code>
            </div>
          )}
          {error !== undefined && (
            <p role="alert" className="action-confirm-error">
              {errorMessage(error, t)}
            </p>
          )}
          <div className="action-confirm-actions">
            <Button disabled={busy} onClick={() => (confirming ? setConfirming(false) : changeOpen(false))}>
              {t(confirming ? 'migrationBack' : 'cancel')}
            </Button>
            <Button
              disabled={disabled || busy || !target}
              className="d-primary"
              aria-busy={busy}
              onClick={() => {
                if (!isCurrent()) return;
                if (confirming) void migrate();
                else setConfirming(true);
              }}
            >
              {t(busy ? 'loading' : confirming ? 'confirm' : 'migrationReview')}
            </Button>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
