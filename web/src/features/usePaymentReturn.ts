import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { APIError, api, query } from '../lib/api';
import { clearPaymentReturn, paymentReturnTarget } from '../lib/payment-return';
import type { Order, Profile } from '../lib/types';

type Phase = 'idle' | 'login' | 'checking' | 'pending' | 'error' | 'confirmed' | 'filing' | 'complete';
type State = { phase: Phase; order?: Order; error?: unknown; confirmationVisible?: boolean };
const minimumStatusMs = 1000;

export function usePaymentReturn() {
  const { profile, loading } = useApp();
  const client = useQueryClient();
  const [target, setTarget] = useState(paymentReturnTarget);
  const [dismissed, setDismissed] = useState(false);
  const [state, setState] = useState<State>({ phase: target.requested ? 'checking' : 'idle' });
  const checkingSince = useRef<number | null>(target.requested ? performance.now() : null);
  const owner = profile?.id ?? '';
  const csrfToken = profile?.csrf_token;

  useEffect(() => {
    if (!target.requested || dismissed || loading) return;
    const controller = new AbortController();
    // A cached identity can change before React cleans up this effect.
    const isCurrent = () => {
      const live = client.getQueryData<Profile | null>(['profile']);
      return (live?.id ?? '') === owner && live?.csrf_token === csrfToken;
    };
    let timer: ReturnType<typeof setTimeout>;
    if (target.id && owner) {
      checkingSince.current ??= performance.now();
      setState({ phase: 'checking' });
    }
    const startedAt = checkingSince.current;
    const settle = (next: State) => {
      const remaining = startedAt === null ? 0 : minimumStatusMs - (performance.now() - startedAt);
      const publish = () => {
        if (controller.signal.aborted || !isCurrent()) return;
        checkingSince.current = null;
        setState(next);
      };
      if (remaining <= 0) publish();
      else timer = setTimeout(publish, Math.ceil(remaining));
    };
    let failures = 0;
    const until = Date.now() + 45000;
    const check = async () => {
      try {
        const order = await api<Order>(
          `order/detail?${query({ id: target.id })}`,
          undefined,
          controller.signal,
          isCurrent,
        );
        if (controller.signal.aborted || !isCurrent()) return;
        // Even admins must only celebrate their own payment; the locator is not authentication.
        if (order.id !== target.id || order.user_id !== owner) throw new APIError('FORBIDDEN', '', 403);
        if (order.status === 'paid') {
          settle({ phase: 'confirmed', order });
          return;
        }
        failures = 0;
        if (order.status === 'closed' || Date.now() >= until) {
          settle({ phase: 'pending', order });
          return;
        }
        timer = setTimeout(() => void check(), 2500);
      } catch (error) {
        if (controller.signal.aborted || !isCurrent()) return;
        const code = error instanceof APIError ? error.code : '';
        if (
          ++failures < 3 &&
          Date.now() < until &&
          ['NETWORK_ERROR', 'SERVER_UNAVAILABLE', 'REQUEST_TIMEOUT', 'INVALID_RESPONSE'].includes(code)
        ) {
          timer = setTimeout(() => void check(), 2500);
        } else settle({ phase: code === 'UNAUTHORIZED' ? 'login' : 'error', error });
      }
    };
    if (!target.id) settle({ phase: 'error', error: new APIError('PAYMENT_RETURN_MISSING', '', 0) });
    else if (!owner) settle({ phase: 'login' });
    else void check();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [target, dismissed, loading, owner, csrfToken, client]);

  useEffect(() => {
    if (state.phase !== 'confirmed' || !state.confirmationVisible) return;
    const timer = setTimeout(
      () => setState((previous) => (previous === state ? { ...previous, phase: 'filing' } : previous)),
      minimumStatusMs,
    );
    return () => clearTimeout(timer);
  }, [state]);

  // AnimatePresence can keep the previous label mounted while its exit finishes.
  // Start the visible second only after this confirmation actually enters.
  const confirmationShown = useCallback(() => {
    setState((previous) =>
      previous === state && previous.phase === 'confirmed' && !previous.confirmationVisible
        ? { ...previous, confirmationVisible: true }
        : previous,
    );
  }, [state]);

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
    confirmationShown,
    finish,
    leave,
  };
}
