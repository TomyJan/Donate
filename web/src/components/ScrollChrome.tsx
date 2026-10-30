import { ArrowUp } from 'lucide-react';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import type { ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { useLocation } from 'react-router';
import { useApp } from '../app/context';
import { Button } from './ui';
import './scroll-chrome.css';

export function ScrollHeader({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const [hidden, setHidden] = useState(false);
  const reduced = useReducedMotion();
  // biome-ignore lint/correctness/useExhaustiveDependencies: navigating resets the scroll-direction tracker.
  useEffect(() => {
    setHidden(false);
    let previous = Math.max(0, scrollY),
      distance = 0,
      direction = 0,
      frame = 0;
    const update = () => {
      frame = 0;
      const position = Math.max(0, scrollY);
      const delta = position - previous;
      previous = position;
      const nextDirection = Math.sign(delta);
      if (nextDirection !== direction) distance = 0;
      direction = nextDirection;
      distance += Math.abs(delta);
      const header = document.querySelector('.d-top');
      if (position < 32 || header?.querySelector(':focus-visible, [data-state=open]')) setHidden(false);
      else if (distance > 10) setHidden(direction > 0);
    };
    const scroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    window.addEventListener('scroll', scroll, { passive: true });
    return () => {
      window.removeEventListener('scroll', scroll);
      cancelAnimationFrame(frame);
    };
  }, [pathname]);
  return (
    <motion.header
      className="d-top"
      data-component="site-header"
      data-hidden={hidden}
      initial={false}
      animate={{ y: hidden ? '-110%' : '0%' }}
      transition={{ duration: reduced ? 0 : 0.23, ease: [0.22, 1, 0.36, 1] }}
      onFocusCapture={() => setHidden(false)}
    >
      {children}
    </motion.header>
  );
}

export function BackToTop() {
  const { t } = useApp();
  const reduced = useReducedMotion();
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const update = () => setVisible(scrollY > 160);
    update();
    window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, []);
  return (
    <AnimatePresence>
      {visible && (
        <motion.div
          className="back-to-top"
          initial={{ opacity: 0, y: reduced ? 0 : 8, scale: reduced ? 1 : 0.94 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: reduced ? 0 : 8 }}
          transition={{ duration: reduced ? 0 : 0.18 }}
        >
          <Button
            className="d-icon"
            title={t('backToTop')}
            aria-label={t('backToTop')}
            onClick={() => window.scrollTo({ top: 0, behavior: reduced ? 'auto' : 'smooth' })}
          >
            <ArrowUp />
          </Button>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
