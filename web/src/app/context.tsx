import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { APIError, api, setCSRF } from '../lib/api';
import { errorMessage } from '../lib/errors';
import type { Language } from '../lib/i18n';
import { translate } from '../lib/i18n';
import { LEGAL_CONSENT_KEY, useLocalAcknowledgment } from '../lib/legal';
import type { Profile, Site } from '../lib/types';

type Toast = { id: string; text: string; error: boolean };
type AppState = {
  site?: Site;
  profile: Profile | null;
  loading: boolean;
  siteError: Error | null;
  lang: Language;
  theme: string;
  t: (key: string) => string;
  setLang: (v: Language) => void;
  setTheme: (v: string) => void;
  loginOpen: boolean;
  legalAccepted: boolean;
  setLegalAccepted: (value: boolean) => void;
  setLoginOpen: (v: boolean) => void;
  accountOpen: boolean;
  setAccountOpen: (v: boolean) => void;
  notify: (text: string, error?: boolean) => void;
  report: (e: unknown) => void;
  refresh: () => Promise<void>;
  retrySite: () => Promise<void>;
  login: (provider: string, bind?: boolean) => Promise<void>;
  logout: () => Promise<void>;
  toasts: Toast[];
  dismiss: (id: string) => void;
};
const Context = createContext<AppState | null>(null);
function readPreference(key: string, fallback: string) {
  try {
    return localStorage.getItem(key) || fallback;
  } catch {
    return fallback;
  }
}
function savePreference(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Preferences remain usable in memory when browser storage is unavailable.
  }
}
export function AppProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const [lang, setLanguage] = useState<Language>(() =>
    readPreference('donate-language', 'zh-CN') === 'en' ? 'en' : 'zh-CN',
  );
  const [theme, setAppearance] = useState(() => readPreference('donate-theme', 'system'));
  useEffect(() => {
    document.documentElement.style.colorScheme = theme === 'system' ? 'light dark' : theme;
  }, [theme]);
  const [loginOpen, setLoginOpen] = useState(false);
  const [legalAccepted, setLegalAccepted] = useLocalAcknowledgment(LEGAL_CONSENT_KEY);
  const [accountOpen, setAccountOpen] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const t = useCallback((key: string) => translate(key, lang), [lang]);
  const dismiss = useCallback((id: string) => setToasts((v) => v.filter((x) => x.id !== id)), []);
  const notify = useCallback(
    (text: string, error = false) => {
      const id = crypto.randomUUID();
      setToasts((v) => [...v.slice(-3), { id, text, error }]);
      setTimeout(() => dismiss(id), error ? 8000 : 4500);
    },
    [dismiss],
  );
  const report = useCallback(
    (e: unknown) => {
      notify(errorMessage(e, t), true);
    },
    [notify, t],
  );
  const site = useQuery({
    queryKey: ['site'],
    queryFn: () => api<Site>('site/info'),
    retry: 1,
    staleTime: 60000,
  });
  const profile = useQuery({
    queryKey: ['profile'],
    queryFn: async () => {
      try {
        return await api<Profile>('user/profile');
      } catch (e) {
        if (e instanceof APIError && (e.status === 401 || e.code === 'ACCOUNT_BANNED')) return null;
        throw e;
      }
    },
    retry: false,
  });
  const current = profile.data ?? null;
  useEffect(() => {
    setCSRF(current?.csrf_token ?? site.data?.csrf_token ?? '');
  }, [current, site.data]);
  const refresh = useCallback(async () => {
    await client.invalidateQueries({ queryKey: ['profile'] });
    await client.invalidateQueries({ queryKey: ['site'] });
    await client.invalidateQueries({ queryKey: ['records'] });
  }, [client]);
  const clearSession = useCallback(() => {
    void client.cancelQueries({ queryKey: ['profile'] });
    client.setQueryData(['profile'], null);
    client.removeQueries({ predicate: (q) => q.queryKey[0] !== 'site' && q.queryKey[0] !== 'profile' });
    setAccountOpen(false);
  }, [client]);
  useEffect(() => {
    const invalidate = (event: Event) => {
      clearSession();
      if ((event as CustomEvent).detail === 'ACCOUNT_BANNED') notify(t('banned'), true);
      void client.invalidateQueries({ queryKey: ['site'] });
    };
    window.addEventListener('session-invalid', invalidate);
    const revokeAdmin = () => {
      client.setQueryData<Profile | null>(['profile'], (value) =>
        value ? { ...value, admin: false } : value,
      );
      client.removeQueries({
        predicate: (q) =>
          String(q.queryKey[0]).startsWith('admin-') ||
          q.queryKey[0] === 'overview' ||
          q.queryKey[0] === 'operation' ||
          (q.queryKey[0] === 'order-detail' && q.queryKey[1] === true),
      });
      void client.invalidateQueries({ queryKey: ['profile'] });
    };
    window.addEventListener('admin-forbidden', revokeAdmin);
    return () => {
      window.removeEventListener('session-invalid', invalidate);
      window.removeEventListener('admin-forbidden', revokeAdmin);
    };
  }, [clearSession, client, notify, t]);
  const currentUserID = current?.id;
  useEffect(() => {
    if (!currentUserID) return;
    let lastRenew = 0,
      lastActivity = Date.now(),
      busy = false;
    const renew = async (force = false) => {
      if (
        busy ||
        document.hidden ||
        (!force && (Date.now() - lastRenew < 300000 || Date.now() - lastActivity > 300000))
      )
        return;
      busy = true;
      try {
        await api('user/session/renew', {});
        lastRenew = Date.now();
      } catch (e) {
        report(e);
      } finally {
        busy = false;
      }
    };
    const active = () => {
      lastActivity = Date.now();
      void renew();
    };
    const visibility = () => {
      if (!document.hidden) {
        lastActivity = Date.now();
        void renew(true);
      }
    };
    void renew(true);
    const timer = setInterval(() => void renew(), 60000);
    for (const type of ['pointerdown', 'keydown', 'touchstart'])
      window.addEventListener(type, active, { passive: true });
    document.addEventListener('visibilitychange', visibility);
    return () => {
      clearInterval(timer);
      for (const type of ['pointerdown', 'keydown', 'touchstart']) window.removeEventListener(type, active);
      document.removeEventListener('visibilitychange', visibility);
    };
  }, [currentUserID, report]);
  const setLang = (v: Language) => {
    savePreference('donate-language', v);
    setLanguage(v);
  };
  const setTheme = (v: string) => {
    savePreference('donate-theme', v);
    setAppearance(v);
  };
  const login = async (provider: string, bind = false) => {
    if (!bind && !legalAccepted) {
      notify(t('acceptPoliciesFirst'), true);
      setLoginOpen(true);
      return;
    }
    try {
      const v = await api<{ authorization_url: string }>(bind ? 'user/identity/bind' : 'user/login', {
        provider,
        return_to: location.pathname.startsWith('/admin') ? '/admin' : '/',
      });
      location.assign(v.authorization_url);
    } catch (e) {
      report(e);
    }
  };
  const logout = async () => {
    try {
      await api('user/logout', {});
      clearSession();
      await refresh();
    } catch (e) {
      report(e);
    }
  };
  return (
    <Context
      value={{
        site: site.data,
        profile: current,
        loading: profile.isLoading || site.isLoading,
        siteError: site.error,
        lang,
        theme,
        t,
        setLang,
        setTheme,
        loginOpen,
        legalAccepted,
        setLegalAccepted,
        setLoginOpen,
        accountOpen,
        setAccountOpen,
        notify,
        report,
        refresh,
        retrySite: () => client.invalidateQueries({ queryKey: ['site'] }),
        login,
        logout,
        toasts,
        dismiss,
      }}
    >
      {children}
    </Context>
  );
}
export function useApp() {
  const v = useContext(Context);
  if (!v) throw new Error('Missing AppProvider');
  return v;
}
