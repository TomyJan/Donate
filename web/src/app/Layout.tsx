import { Check, Languages, LogOut, Monitor, Moon, Settings, Sun, UserRound, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { Suspense, useState } from 'react';
import { Link, Outlet, useLocation, useNavigate } from 'react-router';
import { IdentityBindings } from '../components/IdentityBindings';
import { BackToTop, ScrollHeader } from '../components/ScrollChrome';
import type { MenuItem } from '../components/ui';
import { Avatar, Button, Menu, Modal } from '../components/ui';
import { useApp } from './context';
export function Layout() {
  const app = useApp();
  const { t, profile, site } = app;
  const location = useLocation();
  const navigate = useNavigate();
  const admin = location.pathname.startsWith('/admin');
  const [binding, setBinding] = useState('');
  const themes = [
    ['system', Monitor],
    ['light', Sun],
    ['dark', Moon],
  ] as const;
  const ThemeIcon = themes.find((v) => v[0] === app.theme)?.[1] ?? Monitor;
  const account: MenuItem[] = [
    { label: t('account'), icon: <UserRound />, action: () => app.setAccountOpen(true) },
    ...(profile?.admin
      ? [
          {
            label: t('admin'),
            icon: <Settings />,
            selected: admin,
            action: () => {
              navigate('/admin');
            },
          },
        ]
      : []),
    { label: 'separator', separator: true, action: () => {} },
    { label: t('logout'), icon: <LogOut />, action: () => void app.logout() },
  ];
  const version = site?.version;
  const suffix = version?.tag ? '' : `-${version?.commit.slice(0, 7) ?? 'unknown'}`;
  const versionURL = version?.tag
    ? `https://github.com/TomyJan/Donate/releases/tag/${encodeURIComponent(version.tag)}`
    : `https://github.com/TomyJan/Donate/commit/${version?.commit ?? ''}`;
  return (
    <div id={admin ? 'donate-admin' : 'receipt-refinement'}>
      <div
        className={`d-app ${admin ? 'a-app' : 'r-app'}`}
        lang={app.lang}
        style={{ colorScheme: app.theme === 'system' ? 'light dark' : app.theme }}
      >
        <ScrollHeader>
          <div className="d-identity">
            <Link className="d-brand" to="/">
              <img
                src={site?.logo ?? 'https://public-cdn.tomys.top/head.png'}
                alt=""
                width="30"
                height="30"
              />
              <span>{t('donate')} TomyJan</span>
            </Link>
            {admin && <span className="d-admin-label">{t('admin')}</span>}
          </div>
          <nav className="d-nav" aria-label={app.lang === 'en' ? 'Preferences and account' : '偏好与账号'}>
            <Menu
              label={t('language')}
              trigger={
                <Button className="d-ghost d-icon" aria-label={t('language')}>
                  <Languages />
                </Button>
              }
              items={[
                {
                  label: '简体中文',
                  selected: app.lang === 'zh-CN',
                  icon: <span>中</span>,
                  action: () => app.setLang('zh-CN'),
                },
                {
                  label: 'English',
                  selected: app.lang === 'en',
                  icon: <span>EN</span>,
                  action: () => app.setLang('en'),
                },
              ]}
            />
            <Menu
              label={t('theme')}
              trigger={
                <Button className="d-ghost d-icon" aria-label={t('theme')}>
                  <ThemeIcon />
                </Button>
              }
              items={themes.map(([key, Icon]) => ({
                label: t(key),
                selected: app.theme === key,
                icon: <Icon />,
                action: () => app.setTheme(key),
              }))}
            />
            {profile ? (
              <Menu
                label={t('account')}
                trigger={
                  <Button className="d-profile d-ghost" aria-label={t('account')}>
                    <span className="d-nickname">{profile.nickname}</span>
                    <Avatar name={profile.nickname} src={profile.avatar_url} />
                  </Button>
                }
                items={account}
              />
            ) : (
              <Button
                className="d-ghost d-icon"
                aria-label={t('login')}
                onClick={() => app.setLoginOpen(true)}
              >
                <UserRound />
              </Button>
            )}
          </nav>
        </ScrollHeader>
        <Suspense fallback={<main className="a-main">{t('loading')}</main>}>
          <Outlet />
        </Suspense>
        <footer className="d-footer" data-component="site-footer">
          <a href="https://github.com/TomyJan/Donate" target="_blank" rel="noreferrer">
            Donate
          </a>{' '}
          <a href={versionURL} target="_blank" rel="noreferrer">
            V{version?.version ?? '0.0.1'}
            {suffix}
          </a>
        </footer>
        <Modal open={app.loginOpen} onOpenChange={app.setLoginOpen} title={t('login')}>
          <div className="d-row">
            {site?.providers.map((provider) => (
              <Button
                key={provider}
                className={provider === 'qq' ? 'd-primary' : ''}
                onClick={() => void app.login(provider)}
              >
                {provider.toUpperCase()}
              </Button>
            ))}
            {!site?.providers.length && <p>{t('notConfigured')}</p>}
          </div>
        </Modal>
        <Modal open={app.accountOpen && !!profile} onOpenChange={app.setAccountOpen} title={t('account')}>
          {profile && (
            <>
              <div className="d-person">
                <Avatar name={profile.nickname} src={profile.avatar_url} />
                <div>
                  <strong>{profile.nickname}</strong>
                  <span className="d-account-id">{profile.id}</span>
                </div>
              </div>
              <IdentityBindings bindings={profile.bindings} available={site?.providers} onBind={setBinding} />
            </>
          )}
        </Modal>
        <Modal open={!!binding} onOpenChange={(v) => !v && setBinding('')} title={t('confirmBind')}>
          <p>{t('bindHint')}</p>
          <div className="dialog-actions">
            <Button onClick={() => setBinding('')}>{t('cancel')}</Button>
            <Button className="d-primary" onClick={() => void app.login(binding, true)}>
              {t('confirm')}
            </Button>
          </div>
        </Modal>
        <BackToTop />
        <section className="toast-stack" aria-label={app.lang === 'en' ? 'Notifications' : '消息通知'}>
          <AnimatePresence initial={false}>
            {app.toasts.map((toast) => (
              <motion.div
                layout
                key={toast.id}
                role={toast.error ? 'alert' : 'status'}
                className={`toast ${toast.error ? 'is-error' : ''}`}
                initial={{ opacity: 0, y: 10, scale: 0.98 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, x: 30 }}
              >
                {toast.error ? <X /> : <Check />}
                <span>{toast.text}</span>
                <Button
                  className="d-ghost d-icon"
                  aria-label={t('close')}
                  onClick={() => app.dismiss(toast.id)}
                >
                  <X />
                </Button>
              </motion.div>
            ))}
          </AnimatePresence>
        </section>
      </div>
    </div>
  );
}
