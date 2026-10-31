import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { Profile } from '../lib/types';
import { useApp } from './context';

const sessionKey = (value: Profile | null | undefined) =>
  JSON.stringify([value?.id, value?.csrf_token, value?.admin]);

// Reopening a dialog or returning to the same account must not revive old work.
export function useRequestScope(key: string | null, administrator = false) {
  const { profile } = useApp();
  const client = useQueryClient();
  const scopeKey = JSON.stringify([key, profile?.id, profile?.csrf_token, profile?.admin]);
  const current = useRef({ key: scopeKey });
  if (current.current.key !== scopeKey) current.current = { key: scopeKey };
  const scope = current.current;
  const observedSession = useRef(sessionKey(profile));
  const [, setSessionRevision] = useState(0);
  useLayoutEffect(() => {
    const observe = () => {
      const next = sessionKey(client.getQueryData<Profile | null>(['profile']));
      if (observedSession.current !== next) {
        observedSession.current = next;
        // Revoke synchronously, even if A -> B -> A is batched into one render.
        current.current = { key: current.current.key };
        setSessionRevision((revision) => revision + 1);
      }
    };
    observe();
    return client.getQueryCache().subscribe((event) => {
      if (event.query.queryKey[0] === 'profile') observe();
    });
  }, [client]);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const isCurrent = () => {
    if (!alive.current || current.current !== scope || !key || !profile) return false;
    // Consult the cache as well: another tab may replace credentials before React renders.
    const live = client.getQueryData<Profile | null>(['profile']);
    return (
      live?.id === profile.id &&
      live.csrf_token === profile.csrf_token &&
      live.admin === profile.admin &&
      (!administrator || live.admin)
    );
  };
  return { scopeKey, scope, isCurrent };
}
