import { Check } from 'lucide-react';
import { useId } from 'react';
import { useApp } from '../app/context';
import { loginProviders } from '../lib/providers';
import { LegalLinks } from './LegalLinks';
import { ProviderIcon } from './ProviderIcon';
import { RequestError } from './RequestError';
import { Button, Modal } from './ui';

export function LoginDialog() {
  const app = useApp();
  const { t, site } = app;
  const id = useId();
  return (
    <Modal open={app.loginOpen} onOpenChange={app.setLoginOpen} title={t('login')}>
      <div className="login-providers">
        {loginProviders(site).map((provider) => (
          <Button
            key={provider.id}
            className={provider.kind === 'qq' ? 'd-primary' : ''}
            disabled={!app.legalAccepted}
            onClick={() => void app.login(provider.id)}
          >
            <ProviderIcon provider={provider} />
            <span className="provider-name">{provider.name}</span>
          </Button>
        ))}
        {!site &&
          (app.siteError ? (
            <RequestError error={app.siteError} onRetry={() => void app.retrySite()} />
          ) : (
            <p role="status">{t('loading')}</p>
          ))}
        {site && !site.providers.length && <p>{t('notConfigured')}</p>}
      </div>
      <div className="login-agreement">
        <span className="agreement-check">
          <input
            id={id}
            type="checkbox"
            aria-label={t('acceptPolicies')}
            checked={app.legalAccepted}
            onChange={(event) => app.setLegalAccepted(event.target.checked)}
          />
          <Check aria-hidden="true" />
        </span>
        <span>
          <label htmlFor={id}>{t('readAndAgree')}</label> <LegalLinks newTab />
        </span>
      </div>
    </Modal>
  );
}
