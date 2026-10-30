import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { useEffect, useRef } from 'react';
import { useApp } from '../app/context';
import { LegalLinks } from './LegalLinks';
import { Button } from './ui';

export function CookieNotice({ open, onDismiss }: { open: boolean; onDismiss: () => void }) {
  const { t } = useApp();
  const reduced = useReducedMotion();
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!open || !ref.current) return;
    const element = ref.current;
    const root = element.closest<HTMLElement>('.d-app');
    const measure = () => root?.style.setProperty('--cookie-notice-height', `${element.offsetHeight}px`);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
      root?.style.removeProperty('--cookie-notice-height');
    };
  }, [open]);
  return (
    <AnimatePresence>
      {open && (
        <motion.aside
          ref={ref}
          className="cookie-notice"
          role="region"
          aria-label={t('cookieTitle')}
          initial={{ opacity: 0, y: reduced ? 0 : 18 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: reduced ? 0 : 12 }}
          transition={{ duration: reduced ? 0 : 0.25 }}
        >
          <div className="cookie-copy">
            <h2>{t('cookieTitle')}</h2>
            <p>{t('cookieSummary')}</p>
            <LegalLinks />
          </div>
          <Button className="d-primary" onClick={onDismiss}>
            {t('gotIt')}
          </Button>
        </motion.aside>
      )}
    </AnimatePresence>
  );
}
