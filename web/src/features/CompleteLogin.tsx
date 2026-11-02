import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useApp } from '../app/context';
import { ConstrainedInput } from '../components/ConstrainedInput';
import { Button, Empty } from '../components/ui';
import { acceptQQEdit } from '../lib/amount';
import { APIError, api } from '../lib/api';
import { errorMessage } from '../lib/errors';

const callbackCodes: Record<string, string> = {
  flow_expired: 'AUTH_FLOW_EXPIRED',
  flow_invalid: 'AUTH_FLOW_INVALID',
  attempt_limit: 'AUTH_ATTEMPTS_EXCEEDED',
  session_changed: 'AUTH_SESSION_CHANGED',
  cancelled: 'LOGIN_CANCELLED',
  provider_unavailable: 'LOGIN_PROVIDER_UNAVAILABLE',
  configuration: 'LOGIN_CONFIGURATION_ERROR',
  verification: 'LOGIN_VERIFICATION_FAILED',
  account_banned: 'ACCOUNT_BANNED',
  identity_conflict: 'IDENTITY_CONFLICT',
  rate_limited: 'RATE_LIMITED',
  authorization: 'LOGIN_VERIFICATION_FAILED',
};
const restartCodes = new Set([
  'AUTH_FLOW_EXPIRED',
  'AUTH_FLOW_INVALID',
  'AUTH_ATTEMPTS_EXCEEDED',
  'AUTH_SESSION_CHANGED',
  'UNAUTHORIZED',
  'IDENTITY_CONFLICT',
  'ACCOUNT_BANNED',
]);
export function CompleteLogin() {
  const { t, setLoginOpen, setAccountOpen, report } = useApp();
  const [number, setNumber] = useState(''),
    [busy, setBusy] = useState(false);
  const [submissionError, setSubmissionError] = useState<unknown>(null);
  const status = useQuery({
    queryKey: ['login-flow'],
    queryFn: () =>
      api<{ stage: string; provider: string; intent?: string; return_to: string }>('user/login/status'),
    retry: false,
  });
  // The callback code is presentation-only: it must never invalidate or alter
  // the current profile, which may belong to a different sign-in.
  const callbackError = new URLSearchParams(location.search).get('error');
  const error =
    callbackError !== null
      ? new APIError(
          Object.hasOwn(callbackCodes, callbackError)
            ? callbackCodes[callbackError]
            : 'LOGIN_VERIFICATION_FAILED',
          '',
          0,
        )
      : (submissionError ?? status.error);
  const code = error instanceof APIError ? error.code : '';
  const restart = callbackError !== null || restartCodes.has(code);
  const failed = restart || status.isError;
  const binding = status.data?.intent === 'bind';
  const submit = async () => {
    if (busy || failed || !/^[1-9]\d{4,11}$/.test(number)) return;
    setBusy(true);
    setSubmissionError(null);
    try {
      const result = await api<{ return_to: string }>('user/login/qq/complete', { qq_number: number });
      location.replace(result.return_to);
    } catch (e) {
      setSubmissionError(e);
      report(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <main className="login-complete">
      <article className="d-dialog-frame">
        <h1>{t(failed ? 'loginBanned' : 'qq')}</h1>
        {error != null && submissionError == null && (
          <p role="alert">{code === 'ACCOUNT_BANNED' ? t('loginBannedHint') : errorMessage(error, t)}</p>
        )}
        {failed ? (
          <Button
            disabled={!restart && status.isFetching}
            onClick={() => {
              if (!restart) void status.refetch();
              else if (binding) setAccountOpen(true);
              else setLoginOpen(true);
            }}
          >
            {t(!restart ? 'retry' : binding ? 'account' : 'loginAgain')}
          </Button>
        ) : status.isLoading ? (
          <Empty text={t('loading')} />
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <p>{t(binding ? 'qqBindHint' : 'qqHint')}</p>
            <ConstrainedInput
              className="d-input"
              aria-label={t('qq')}
              inputMode="numeric"
              autoComplete="off"
              value={number}
              onValue={setNumber}
              accept={acceptQQEdit}
            />
            <Button
              type="submit"
              className="d-primary d-qq-submit"
              disabled={busy || !/^[1-9]\d{4,11}$/.test(number)}
            >
              {t('finish')}
            </Button>
          </form>
        )}
      </article>
    </main>
  );
}
