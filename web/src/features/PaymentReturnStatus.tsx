import { Check, LoaderCircle } from 'lucide-react';
import { motion, useReducedMotion } from 'motion/react';
import { useApp } from '../app/context';
import { Button } from '../components/ui';
import { errorMessage } from '../lib/errors';
import type { usePaymentReturn } from './usePaymentReturn';

export function PaymentReturnLabel({ payment }: { payment: ReturnType<typeof usePaymentReturn> }) {
  const { t } = useApp();
  const confirmed = payment.phase === 'confirmed' || payment.phase === 'filing';
  const text =
    payment.phase === 'login'
      ? t('paymentSignIn')
      : payment.phase === 'error'
        ? `${t('paymentCheckFailed')} · ${errorMessage(payment.error, t)}`
        : payment.phase === 'pending'
          ? t('paymentUnconfirmed')
          : payment.phase === 'confirmed'
            ? t('paymentConfirmed')
            : payment.phase === 'filing'
              ? t('paymentFiling')
              : t('paymentChecking');
  return (
    <>
      {confirmed ? (
        <Check aria-hidden="true" />
      ) : payment.phase === 'checking' ? (
        <LoaderCircle className="payment-spinner" aria-hidden="true" />
      ) : null}
      <span>{text}</span>
    </>
  );
}

export function PaymentReturnStatus({ payment }: { payment: ReturnType<typeof usePaymentReturn> }) {
  const { t, setLoginOpen } = useApp();
  const reduced = useReducedMotion();
  if (!payment.locked || payment.phase === 'confirmed' || payment.phase === 'filing') return null;
  return (
    <div className="payment-return-status" data-state={payment.phase}>
      <motion.div
        className="payment-return-actions"
        initial={reduced ? false : { opacity: 0, y: 4 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: reduced ? 0 : 0.28 }}
      >
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
      </motion.div>
    </div>
  );
}
