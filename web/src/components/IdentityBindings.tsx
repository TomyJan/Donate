import { useApp } from '../app/context';
import type { Profile } from '../lib/types';
import { Avatar, Button } from './ui';
export function IdentityBindings({
  bindings,
  available = [],
  onBind,
}: {
  bindings: Profile['bindings'];
  available?: string[];
  onBind?: (provider: string) => void;
}) {
  const { t } = useApp();
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
            </div>
            <div className="d-bind-action">
              {identity ? (
                t('bound')
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
    </div>
  );
}
