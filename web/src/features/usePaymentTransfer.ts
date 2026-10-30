import type { RefObject } from 'react';
import { useEffect, useRef } from 'react';

type Props = {
  active: boolean;
  reduced: boolean;
  composer: RefObject<HTMLElement | null>;
  destination: RefObject<HTMLElement | null>;
  onComplete: () => void;
};

export function usePaymentTransfer({ active, reduced, composer, destination, onComplete }: Props) {
  const complete = useRef(onComplete);
  complete.current = onComplete;
  useEffect(() => {
    if (!active) return;
    const source = composer.current;
    const target = destination.current;
    if (!source || !target || reduced) {
      complete.current();
      return;
    }
    let stopped = false;
    let animation: Animation | undefined;
    const travel = async () => {
      try {
        source.style.zIndex = '20';
        source.style.transformOrigin = '50% 50%';
        animation = source.animate(
          [
            { transform: 'perspective(1000px) rotateX(0deg) scale(1)', filter: 'brightness(1)' },
            { transform: 'perspective(1000px) rotateX(52deg) scale(.84,.52)', filter: 'brightness(.96)' },
          ],
          { duration: 900, easing: 'cubic-bezier(.4,0,.2,1)', fill: 'forwards' },
        );
        await animation.finished;
        if (stopped) return;
        const from = source.getBoundingClientRect();
        const to = target.getBoundingClientRect();
        const dx = to.left + to.width / 2 - from.left - from.width / 2;
        const dy = to.top + Math.min(to.height / 2, 160) - from.top - from.height / 2;
        const frames: Keyframe[] = [];
        // Cubic Bézier with a gentle lift; vertical layouts bend sideways instead.
        const narrow = Math.abs(dx) < source.offsetWidth / 2;
        const c1 = { x: narrow ? -36 : dx * 0.2, y: narrow ? dy * 0.2 : -95 };
        const c2 = { x: narrow ? 42 : dx * 0.75, y: narrow ? dy * 0.7 : dy - 80 };
        for (let step = 0; step <= 30; step++) {
          const t = step / 30,
            u = 1 - t;
          const x = 3 * u * u * t * c1.x + 3 * u * t * t * c2.x + t * t * t * dx;
          const y = 3 * u * u * t * c1.y + 3 * u * t * t * c2.y + t * t * t * dy;
          const angle = -10 * Math.sin(t * Math.PI) + 2 * t;
          frames.push({
            offset: t,
            transform:
              'perspective(1000px) translate(' +
              x +
              'px,' +
              y +
              'px) rotateZ(' +
              angle +
              'deg) rotateX(' +
              (52 - 30 * t) +
              'deg) scale(' +
              (0.84 - 0.12 * t) +
              ',' +
              (0.52 - 0.3 * t) +
              ')',
            opacity: t > 0.84 ? 1 - (t - 0.84) / 0.16 : 1,
            filter: 'brightness(.96)',
          });
        }
        animation.cancel();
        animation = source.animate(frames, {
          duration: 2350,
          easing: 'cubic-bezier(.35,0,.2,1)',
          fill: 'forwards',
        });
        if (narrow && (to.bottom > innerHeight || to.top < 80))
          target.scrollIntoView({ behavior: 'smooth', block: 'center' });
        await animation.finished;
        if (!stopped) complete.current();
      } catch {
        // Interrupted animation must not leave a confirmed payment stuck or the form unusable.
        if (!stopped) complete.current();
      }
    };
    void travel();
    return () => {
      stopped = true;
      animation?.cancel();
      source.style.removeProperty('z-index');
      source.style.removeProperty('transform-origin');
    };
  }, [active, reduced, composer, destination]);
}
