import { ArrowUp } from 'lucide-react';
import { AnimatePresence, motion, useMotionValue, useReducedMotion } from 'motion/react';
import type { ReactNode } from 'react';
import { useEffect, useRef, useState } from 'react';
import { useLocation } from 'react-router';
import { useApp } from '../app/context';
import { Button } from './ui';
import './scroll-chrome.css';

export function ScrollHeader({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const [hidden, setHidden] = useState(false);
  const header = useRef<HTMLElement>(null);
  const offset = useMotionValue(0);
  const reveal = () => {
    offset.set(0);
    setHidden(false);
  };
  // biome-ignore lint/correctness/useExhaustiveDependencies: navigating resets the scroll-direction tracker.
  useEffect(() => {
    offset.set(0);
    setHidden(false);
    let previous = Math.max(0, scrollY),
      frame = 0;
    const update = () => {
      frame = 0;
      const position = Math.max(0, scrollY);
      const delta = position - previous;
      previous = position;
      const element = header.current;
      if (!element) return;
      const height = element.offsetHeight;
      const locked = element.querySelector(':focus-visible, [data-state=open]');
      const next = position < 32 || locked ? 0 : Math.min(0, Math.max(-height, offset.get() - delta));
      offset.set(next);
      setHidden(height > 0 && next <= -height);
    };
    const scroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    window.addEventListener('scroll', scroll, { passive: true });
    return () => {
      window.removeEventListener('scroll', scroll);
      cancelAnimationFrame(frame);
    };
  }, [pathname, offset]);
  return (
    <motion.header
      ref={header}
      className="d-top"
      data-component="site-header"
      data-hidden={hidden}
      initial={false}
      style={{ y: offset }}
      onFocusCapture={reveal}
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
    let previous = Math.max(0, scrollY);
    let upward = 0;
    const update = () => {
      const current = Math.max(0, scrollY);
      const delta = current - previous;
      previous = current;
      if (current <= 160 || delta > 0) {
        upward = 0;
        setVisible(false);
      } else if (delta < 0) {
        upward -= delta;
        if (upward >= 8) setVisible(true);
      }
    };
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
