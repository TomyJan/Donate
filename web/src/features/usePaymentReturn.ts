import { useCallback, useEffect, useState } from 'react';
import { useApp } from '../app/context';
import { APIError, api, query } from '../lib/api';
import { clearPaymentReturn, paymentReturnTarget } from '../lib/payment-return';
import type { Order } from '../lib/types';

type Phase = 'idle' | 'login' | 'checking' | 'pending' | 'error' | 'confirmed' | 'filing' | 'complete';
type State = { phase: Phase; order?: Order; error?: unknown };

export function usePaymentReturn() {
  const { profile, loading } = useApp();
  const [target, setTarget] = useState(paymentReturnTarget);
  const [dismissed, setDismissed] = useState(false);
  const [state, setState] = useState<State>({ phase: target.requested ? 'checking' : 'idle' });
  const owner = profile?.id ?? '';

  useEffect(() => {
    if (!target.requested || dismissed || loading) return;
    if (!target.id) {
      setState({ phase: 'error', error: new APIError('PAYMENT_RETURN_MISSING', '', 0) });
      return;
    }
    if (!owner) {
      setState({ phase: 'login' });
      return;
    }
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let failures = 0;
    const until = Date.now() + 45000;
    setState({ phase: 'checking' });
    const check = async () => {
      try {
        const order = await api<Order>(
          `order/detail?${query({ id: target.id })}`,
          undefined,
          controller.signal,
        );
        if (controller.signal.aborted) return;
        // Even admins must only celebrate their own payment; the locator is not authentication.
        if (order.id !== target.id || order.user_id !== owner) throw new APIError('FORBIDDEN', '', 403);
        if (order.status === 'paid') {
          setState({ phase: 'confirmed', order });
          return;
        }
        failures = 0;
        if (order.status === 'closed' || Date.now() >= until) {
          setState({ phase: 'pending', order });
          return;
        }
        timer = setTimeout(() => void check(), 2500);
      } catch (error) {
        if (controller.signal.aborted) return;
        const code = error instanceof APIError ? error.code : '';
        if (
          ++failures < 3 &&
          Date.now() < until &&
          ['NETWORK_ERROR', 'SERVER_UNAVAILABLE', 'REQUEST_TIMEOUT', 'INVALID_RESPONSE'].includes(code)
        ) {
          timer = setTimeout(() => void check(), 2500);
        } else setState({ phase: code === 'UNAUTHORIZED' ? 'login' : 'error', error });
      }
    };
    void check();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [target, dismissed, loading, owner]);

  useEffect(() => {
    if (state.phase !== 'confirmed') return;
    const timer = setTimeout(
      () =>
        setState((previous) =>
          previous.phase === 'confirmed' ? { ...previous, phase: 'filing' } : previous,
        ),
      1000,
    );
    return () => clearTimeout(timer);
  }, [state.phase]);

  const finish = useCallback(() => {
    clearPaymentReturn(target.id);
    setDismissed(true);
    setState((previous) => ({ ...previous, phase: 'complete' }));
  }, [target.id]);
  const leave = () => {
    clearPaymentReturn(target.id);
    setDismissed(true);
    setState({ phase: 'idle' });
  };
  return {
    ...state,
    id: target.id,
    locked: state.phase !== 'idle' && state.phase !== 'complete',
    retry: () => setTarget((previous) => ({ ...previous })),
    finish,
    leave,
  };
}
