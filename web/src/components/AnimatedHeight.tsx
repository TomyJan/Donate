import type { ReactNode } from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
export function AnimatedHeight({ children, as: Tag = 'div' }: { children: ReactNode; as?: 'div' | 'span' }) {
  const content = useRef<HTMLElement>(null);
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
    <Tag className="animated-height" style={{ height }}>
      <Tag
        className="animated-height-content"
        ref={(element) => {
          content.current = element;
        }}
      >
        {children}
      </Tag>
    </Tag>
  );
}
