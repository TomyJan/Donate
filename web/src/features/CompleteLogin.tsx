import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useApp } from '../app/context';
import { ConstrainedInput } from '../components/ConstrainedInput';
import { Button, Empty } from '../components/ui';
import { acceptQQEdit } from '../lib/amount';
import { api } from '../lib/api';
export function CompleteLogin() {
  const { t, report, setLoginOpen } = useApp();
  const [number, setNumber] = useState(''),
    [busy, setBusy] = useState(false);
  const status = useQuery({
    queryKey: ['login-flow'],
    queryFn: () => api<{ stage: string; provider: string; return_to: string }>('user/login/status'),
    retry: false,
  });
  // The callback code is presentation-only: it must never invalidate or alter
  // the current profile, which may belong to a different sign-in.
  const callbackError = new URLSearchParams(location.search).get('error');
  const failed = callbackError !== null || status.isError;
  const submit = async () => {
    setBusy(true);
    try {
      const result = await api<{ return_to: string }>('user/login/qq/complete', { qq_number: number });
      location.replace(result.return_to);
    } catch (e) {
      report(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <main className="login-complete">
      <article className="d-dialog-frame">
        <h1>{t(failed ? (callbackError === 'account_banned' ? 'loginBanned' : 'loginError') : 'qq')}</h1>
        {callbackError === 'account_banned' && <p role="alert">{t('loginBannedHint')}</p>}
        {failed ? (
          <Button onClick={() => setLoginOpen(true)}>{t('login')}</Button>
        ) : status.isLoading ? (
          <Empty text={t('loading')} />
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <p>{t('qqHint')}</p>
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
