import { ArrowRight, LogIn, ShieldAlert } from 'lucide-react';
import { motion, useReducedMotion } from 'motion/react';
import { useId } from 'react';
import { Link } from 'react-router';
import { useApp } from '../app/context';
import { Button } from './ui';
import './AccessState.css';

export function AccessState({ signedIn, onLogin }: { signedIn: boolean; onLogin: () => void }) {
  const { t } = useApp();
  const reduced = useReducedMotion();
  const title = useId();
  const Icon = signedIn ? ShieldAlert : LogIn;
  return (
    <main className="access-page">
      <motion.section
        className="access-card"
        aria-labelledby={title}
        initial={reduced ? false : { opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.24 }}
      >
        <span className="access-symbol" aria-hidden="true">
          <Icon />
        </span>
        <p className="access-context">{t('admin')}</p>
        <h1 id={title}>{t(signedIn ? 'forbidden' : 'unauthorized')}</h1>
        <p className="access-hint">{t(signedIn ? 'adminDeniedHint' : 'adminSignInHint')}</p>
        <div className="access-actions">
          {!signedIn && (
            <Button className="d-primary" onClick={onLogin}>
              {t('login')}
              <ArrowRight />
            </Button>
          )}
          <Link to="/" className="d-btn d-control d-ghost">
            {t('home')}
          </Link>
        </div>
      </motion.section>
    </main>
  );
}
