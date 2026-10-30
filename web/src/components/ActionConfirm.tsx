import * as Popover from '@radix-ui/react-popover';
import {
  cloneElement,
  type ReactElement,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';
import { useApp } from '../app/context';
import { Button, portal } from './ui';
import './ActionConfirm.css';

export type ActionConfirmProps<T = undefined> = {
  trigger: ReactElement<{ disabled?: boolean }>;
  title: string;
  description?: string;
  prepare?: () => Promise<T>;
  onConfirm: (prepared: T) => Promise<void>;
  expiresAt?: (prepared: T) => string | number | undefined;
  scopeKey?: string;
  danger?: boolean;
  children?: ReactNode;
};

export function ActionConfirm<T = undefined>({
  trigger,
  title,
  description,
  prepare,
  onConfirm,
  expiresAt,
  scopeKey,
  danger = false,
  children,
}: ActionConfirmProps<T>) {
  const { t, report } = useApp();
  const [open, setOpen] = useState(false);
  const [preparing, setPreparing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [prepared, setPrepared] = useState<{ value: T }>();
  const [error, setError] = useState('');
  const disabled = !!trigger.props.disabled;
  const version = useRef(0);
  const working = useRef(false);
  const descriptionID = useId();
  const invalidate = useCallback(() => {
    version.current += 1;
    working.current = false;
    setPrepared(undefined);
    setPreparing(false);
    setBusy(false);
    setError('');
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Changed scope revokes prepared credentials and pending preview results.
  useEffect(() => {
    setOpen(false);
    invalidate();
    return () => {
      version.current += 1;
    };
  }, [scopeKey, invalidate]);
  useEffect(() => {
    if (!disabled || busy) return;
    setOpen(false);
    invalidate();
  }, [disabled, busy, invalidate]);
  const prepareCurrent = async () => {
    if (disabled) return false;
    const current = ++version.current;
    setPrepared(undefined);
    setError('');
    setPreparing(true);
    working.current = true;
    try {
      const value = prepare ? await prepare() : (undefined as T);
      if (current !== version.current) return false;
      setPrepared({ value });
      return true;
    } catch (failure) {
      if (current === version.current) {
        setError(t('error'));
        report(failure);
      }
      return false;
    } finally {
      if (current === version.current) {
        setPreparing(false);
        working.current = false;
      }
    }
  };
  const changeOpen = (value: boolean) => {
    if (busy || (value && disabled)) return;
    setOpen(value);
    if (value) void prepareCurrent();
    else invalidate();
  };
  const confirm = async () => {
    if (disabled || !prepared || working.current) return;
    const expiry = expiresAt?.(prepared.value);
    const expiryTime = typeof expiry === 'string' ? Date.parse(expiry) : expiry;
    if (expiryTime !== undefined && (!Number.isFinite(expiryTime) || expiryTime <= Date.now())) {
      if (await prepareCurrent()) setError(t('confirmationExpired'));
      return;
    }
    const current = version.current;
    working.current = true;
    setBusy(true);
    setError('');
    try {
      await onConfirm(prepared.value);
      if (current !== version.current) return;
      setOpen(false);
      invalidate();
    } catch (failure) {
      if (current === version.current) {
        setError(t('error'));
        report(failure);
      }
    } finally {
      if (current === version.current) {
        working.current = false;
        setBusy(false);
      }
    }
  };
  return (
    <Popover.Root open={open} onOpenChange={changeOpen}>
      <Popover.Trigger asChild>
        {cloneElement(trigger, { disabled: trigger.props.disabled || busy })}
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content
          className="action-confirm d-pop"
          sideOffset={8}
          collisionPadding={12}
          align="end"
          aria-label={title}
          aria-describedby={description || children ? descriptionID : undefined}
          aria-busy={preparing || busy}
          onEscapeKeyDown={(event) => {
            if (busy) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (busy) event.preventDefault();
          }}
        >
          <p
            className={`action-confirm-prompt ${error ? 'action-confirm-error' : ''}`}
            role={error ? 'alert' : undefined}
          >
            {error || t('confirmActionPrompt').replace('{action}', title)}
          </p>
          {(description || children) && (
            <div id={descriptionID} hidden>
              {description}
              {children}
            </div>
          )}
          <span role="status" className="action-confirm-status">
            {preparing ? t('loading') : ''}
          </span>
          <div className="action-confirm-actions">
            <Button disabled={busy} onClick={() => changeOpen(false)}>
              {t('cancel')}
            </Button>
            <Button
              disabled={disabled || preparing || busy || !prepared}
              className={danger ? 'is-danger' : 'd-primary'}
              aria-busy={preparing || busy}
              onClick={() => void confirm()}
            >
              {busy ? t('loading') : t('confirm')}
            </Button>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
