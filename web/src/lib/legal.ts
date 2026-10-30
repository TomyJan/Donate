import { useCallback, useEffect, useState } from 'react';

// Bump this when either document materially changes: previous agreement must not silently carry over.
export const LEGAL_VERSION = '2026-10-30';
export const LEGAL_CONSENT_KEY = 'donate:legal-consent';
export const COOKIE_NOTICE_KEY = 'donate:cookie-notice';

export function useLocalAcknowledgment(key: string) {
  const read = useCallback(() => {
    try {
      return localStorage.getItem(key) === LEGAL_VERSION;
    } catch {
      return false;
    }
  }, [key]);
  const [accepted, setAccepted] = useState(read);
  useEffect(() => {
    const sync = (event: StorageEvent) => {
      if (event.key === key || event.key === null) setAccepted(read());
    };
    window.addEventListener('storage', sync);
    return () => window.removeEventListener('storage', sync);
  }, [key, read]);
  const update = useCallback(
    (value: boolean) => {
      setAccepted(value);
      try {
        if (value) localStorage.setItem(key, LEGAL_VERSION);
        else localStorage.removeItem(key);
      } catch {
        // Storage can be disabled. The current tab still works; a new visit asks again.
      }
    },
    [key],
  );
  return [accepted, update] as const;
}
