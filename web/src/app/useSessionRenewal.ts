import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { APIError, api } from '../lib/api';
import type { Profile } from '../lib/types';

const renewalWindow = 20 * 86400000;
const activityWindow = 5 * 60000;
const renewedStorageKey = 'donate-session-renewed';
const renewals = new Map<string, Promise<Profile | null | undefined>>();

export function useSessionRenewal(
  profile: Profile | null,
  clearSession: () => void,
  report: (error: unknown) => void,
) {
  const client = useQueryClient();
  const identity = profile ? `${profile.id}:${profile.csrf_token}` : '';
  const activity = useRef({ identity, at: Date.now(), retryAfter: 0 });
  if (activity.current.identity !== identity) activity.current = { identity, at: Date.now(), retryAfter: 0 };
  const userID = profile?.id;
  const csrfToken = profile?.csrf_token;
  const expiresAt = profile?.expires_at;

  useEffect(() => {
    if (!userID || !csrfToken || !expiresAt) return;
    let disposed = false;
    let busy = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const liveProfile = () => {
      const value = client.getQueryData<Profile | null>(['profile']);
      return value?.id === userID && value.csrf_token === csrfToken ? value : null;
    };
    const active = () => !document.hidden && Date.now() - activity.current.at <= activityWindow;
    const renew = async () => {
      const live = liveProfile();
      const expiry = Date.parse(live?.expires_at ?? '');
      if (
        disposed ||
        busy ||
        !live ||
        !active() ||
        !Number.isFinite(expiry) ||
        expiry - Date.now() > renewalWindow ||
        Date.now() < activity.current.retryAfter
      )
        return;
      busy = true;
      try {
        let pending = renewals.get(identity);
        if (!pending) {
          const readProfile = async () => {
            try {
              return await api<Profile>('user/profile');
            } catch (error) {
              if (error instanceof APIError && (error.status === 401 || error.code === 'ACCOUNT_BANNED'))
                return null;
              throw error;
            }
          };
          const refresh = async () => {
            if (disposed || !active() || !liveProfile()) return undefined;
            activity.current.retryAfter = Date.now() + 60000;
            // Re-read while holding the cross-tab lock: a business POST or another
            // tab may have extended the cookie since this tab loaded its profile.
            // Do not let a delayed response install itself into the shared cache
            // before checking whether this request's credentials are still live.
            let fresh = await readProfile();
            if (disposed || !liveProfile()) return undefined;
            if (!fresh || fresh.id !== userID || fresh.csrf_token !== csrfToken) return fresh;
            const remaining = Date.parse(fresh.expires_at) - Date.now();
            if (remaining > 0 && remaining <= renewalWindow && active() && liveProfile()) {
              await api('user/session/renew', {});
              if (disposed || !liveProfile()) return undefined;
              fresh = await readProfile();
              if (disposed || !liveProfile()) return undefined;
            }
            return fresh;
          };
          pending = Promise.resolve()
            .then(() =>
              navigator.locks
                ? navigator.locks.request(`donate-session-renew:${userID}`, refresh)
                : refresh(),
            )
            .finally(() => {
              renewals.delete(identity);
            });
          renewals.set(identity, pending);
        }
        const fresh = await pending;
        if (!fresh) {
          if (fresh === null && !disposed && liveProfile()) clearSession();
        } else if (!disposed && liveProfile() && fresh.id === userID && fresh.csrf_token === csrfToken) {
          client.setQueryData(['profile'], fresh);
          if (fresh.expires_at !== live.expires_at) {
            try {
              localStorage.setItem(
                renewedStorageKey,
                JSON.stringify({
                  id: userID,
                  previous_expires_at: live.expires_at,
                  expires_at: fresh.expires_at,
                }),
              );
            } catch {
              // The native cross-tab lock still prevents duplicate renewal without storage.
            }
          }
        }
      } catch (error) {
        if (!disposed && liveProfile()) report(error);
      } finally {
        busy = false;
      }
    };
    const onActivity = () => {
      activity.current.at = Date.now();
      void renew();
    };
    const onVisibility = () => {
      if (!document.hidden) onActivity();
    };
    const onStorage = (event: StorageEvent) => {
      if (event.key !== renewedStorageKey || !event.newValue) return;
      try {
        const value = JSON.parse(event.newValue);
        const live = liveProfile();
        if (
          live &&
          value.id === userID &&
          value.previous_expires_at === live.expires_at &&
          typeof value.expires_at === 'string' &&
          Date.parse(value.expires_at) > Date.parse(live.expires_at)
        )
          client.setQueryData(['profile'], { ...live, expires_at: value.expires_at });
      } catch {
        // Ignore storage events written by an older or unrelated application.
      }
    };
    const schedule = () => {
      const delay = Date.parse(liveProfile()?.expires_at ?? '') - Date.now() - renewalWindow;
      if (delay > 0)
        timer = setTimeout(
          () => {
            void renew();
            schedule();
          },
          Math.min(delay, 2147483647),
        );
    };
    schedule();
    void renew();
    for (const type of ['pointerdown', 'keydown', 'touchstart', 'focus'])
      window.addEventListener(type, onActivity, { passive: true });
    document.addEventListener('visibilitychange', onVisibility);
    window.addEventListener('storage', onStorage);
    return () => {
      disposed = true;
      clearTimeout(timer);
      for (const type of ['pointerdown', 'keydown', 'touchstart', 'focus'])
        window.removeEventListener(type, onActivity);
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('storage', onStorage);
    };
  }, [client, userID, csrfToken, expiresAt, identity, clearSession, report]);
}
