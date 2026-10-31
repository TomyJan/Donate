import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import type { Profile } from '../lib/types';
import { useApp } from './context';

// Reopening a dialog or returning to the same account must not revive old work.
export function useRequestScope(key: string | null, administrator = false) {
  const { profile } = useApp();
  const client = useQueryClient();
  const scopeKey = JSON.stringify([key, profile?.id, profile?.csrf_token, profile?.admin]);
  const current = useRef({ key: scopeKey });
  if (current.current.key !== scopeKey) current.current = { key: scopeKey };
  const scope = current.current;
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
