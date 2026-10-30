import { RefreshCw, WifiOff } from 'lucide-react';
import { useApp } from '../app/context';
import { APIError } from '../lib/api';
import { errorMessage } from '../lib/errors';
import { Button } from './ui';
import './request-error.css';

export function RequestError({
  error,
  onRetry,
  busy = false,
}: {
  error: unknown;
  onRetry: () => void;
  busy?: boolean;
}) {
  const { t, setLoginOpen } = useApp();
  const code = error instanceof APIError ? error.code : '';
  const login = code === 'UNAUTHORIZED';
  const canRetry = !['FORBIDDEN', 'ACCOUNT_BANNED', 'NOT_FOUND', 'VALIDATION_FAILED'].includes(code);
  return (
    <div className="request-error" role="status">
      {code === 'OFFLINE' && <WifiOff aria-hidden="true" />}
      <p>{errorMessage(error, t)}</p>
      {canRetry && (
        <Button className="d-ghost" disabled={busy} onClick={login ? () => setLoginOpen(true) : onRetry}>
          {!login && <RefreshCw className={busy ? 'is-spinning' : ''} />}
          {t(login ? 'login' : busy ? 'loading' : 'retry')}
        </Button>
      )}
    </div>
  );
}
