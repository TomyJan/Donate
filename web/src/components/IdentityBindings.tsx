import { useId, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { dateTime } from '../lib/api';
import type { Profile } from '../lib/types';
import { ActionConfirm } from './ActionConfirm';
import { Avatar, Button } from './ui';
import './IdentityBindings.css';
export function IdentityBindings({
  bindings,
  available = [],
  onBind,
  ownerID,
  onUnbind,
}: {
  bindings: Profile['bindings'];
  available?: string[];
  onBind?: (provider: string) => void;
  ownerID?: string;
  onUnbind?: (identityID: string) => Promise<void>;
}) {
  const { t, profile } = useApp();
  const [busy, setBusy] = useState(false);
  const working = useRef(false);
  const lastIdentityID = useId();
  const scopeKey = JSON.stringify([ownerID, bindings.map((binding) => binding.id)]);
  const current = useRef({ scopeKey, bindings, authorized: !!profile?.admin && !!onUnbind });
  current.current = { scopeKey, bindings, authorized: !!profile?.admin && !!onUnbind };
  const validate = (identityID: string, scope: string) => {
    if (
      !current.current.authorized ||
      current.current.scopeKey !== scope ||
      working.current ||
      !current.current.bindings.some((binding) => binding.id === identityID)
    )
      throw new Error(t('error'));
    if (current.current.bindings.length < 2) throw new Error(t('lastIdentityHint'));
    return scope;
  };
  return (
    <div className="d-bindings">
      {['qq', 'oidc'].map((provider) => {
        const identity = bindings.find((binding) => binding.provider === provider);
        return (
          <div className="d-bind" key={provider}>
            {identity ? (
              <Avatar name={identity.nickname} src={identity.avatar_url} />
            ) : (
              <span className="d-avatar">{provider === 'qq' ? 'Q' : 'O'}</span>
            )}
            <div className="d-bind-info">
              <p>{provider.toUpperCase()}</p>
              {identity && <span className="d-secondary">{identity.nickname}</span>}
              {identity && (
                <span className="d-bind-time">
                  {t('boundAt')} <time dateTime={identity.bound_at}>{dateTime(identity.bound_at)}</time>
                </span>
              )}
            </div>
            <div className="d-bind-action">
              {identity ? (
                profile?.admin && onUnbind ? (
                  <ActionConfirm
                    scopeKey={scopeKey + identity.id}
                    title={`${t('unbind')} ${provider.toUpperCase()}`}
                    description={t('unbindHint')}
                    danger
                    trigger={
                      <Button
                        className="is-danger"
                        disabled={busy || bindings.length < 2}
                        aria-describedby={bindings.length < 2 ? lastIdentityID : undefined}
                      >
                        {t('unbind')} {provider.toUpperCase()}
                      </Button>
                    }
                    prepare={async () => validate(identity.id, scopeKey)}
                    onConfirm={async (scope) => {
                      validate(identity.id, scope);
                      working.current = true;
                      setBusy(true);
                      try {
                        await onUnbind(identity.id);
                      } finally {
                        working.current = false;
                        setBusy(false);
                      }
                    }}
                  />
                ) : (
                  t('bound')
                )
              ) : onBind ? (
                <Button disabled={!available.includes(provider)} onClick={() => onBind(provider)}>
                  {t('bind')}
                </Button>
              ) : (
                t('unbound')
              )}
            </div>
          </div>
        );
      })}
      {profile?.admin && onUnbind && bindings.length < 2 && (
        <span id={lastIdentityID} className="d-sr">
          {t('lastIdentityHint')}
        </span>
      )}
    </div>
  );
}
