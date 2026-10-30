import type { ReactNode } from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
export function AnimatedHeight({ children }: { children: ReactNode }) {
  const content = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number>();
  useLayoutEffect(() => {
    const element = content.current;
    if (!element) return;
    const measure = () => setHeight(element.offsetHeight);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  return (
    <div className="animated-height" style={{ height }}>
      <div className="animated-height-content" ref={content}>
        {children}
      </div>
    </div>
  );
}
