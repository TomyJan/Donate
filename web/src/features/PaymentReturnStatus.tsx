import { Check, LoaderCircle } from 'lucide-react';
import { useApp } from '../app/context';
import { Button } from '../components/ui';
import { errorMessage } from '../lib/errors';
import type { usePaymentReturn } from './usePaymentReturn';

export function PaymentReturnStatus({ payment }: { payment: ReturnType<typeof usePaymentReturn> }) {
  const { t, setLoginOpen } = useApp();
  if (!payment.locked) return null;
  const confirmed = payment.phase === 'confirmed' || payment.phase === 'filing';
  const text =
    payment.phase === 'login'
      ? t('paymentSignInHint')
      : payment.phase === 'error'
        ? errorMessage(payment.error, t)
        : payment.phase === 'pending'
          ? t('paymentPendingHint')
          : confirmed
            ? t('paymentThanks')
            : t('paymentCheckingHint');
  return (
    <div className="payment-return-status" data-state={payment.phase}>
      <p role="status" aria-live="polite">
        {confirmed ? (
          <Check aria-hidden="true" />
        ) : payment.phase === 'checking' ? (
          <LoaderCircle className="payment-spinner" aria-hidden="true" />
        ) : null}
        <span>{text}</span>
      </p>
      {!confirmed && (
        <div className="payment-return-actions">
          {payment.phase === 'login' ? (
            <Button className="d-ghost" onClick={() => setLoginOpen(true)}>
              {t('login')}
            </Button>
          ) : (payment.phase === 'pending' || payment.phase === 'error') && payment.id ? (
            <Button className="d-ghost" onClick={payment.retry}>
              {t('paymentRetry')}
            </Button>
          ) : null}
          <Button className="d-ghost" onClick={payment.leave}>
            {t('paymentLater')}
          </Button>
        </div>
      )}
    </div>
  );
}
