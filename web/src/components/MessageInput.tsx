import type { RefObject } from 'react';
import { useCallback, useLayoutEffect, useRef } from 'react';

type Props = {
  value: string;
  label: string;
  onValue: (value: string) => void;
  composer: RefObject<HTMLElement | null>;
  book: RefObject<HTMLElement | null>;
};

export function MessageInput({ value, label, onValue, composer, book }: Props) {
  const input = useRef<HTMLTextAreaElement>(null);
  const resize = useCallback(() => {
    const el = input.current;
    const card = composer.current;
    const records = book.current;
    if (!el || !card || !records || !el.offsetWidth) return;
    const css = getComputedStyle(el);
    const line = Number.parseFloat(css.lineHeight);
    const padding = Number.parseFloat(css.paddingTop) + Number.parseFloat(css.paddingBottom);
    const minimum = Math.ceil(line * 2 + padding);
    const mirror = el.cloneNode(false) as HTMLTextAreaElement;
    mirror.removeAttribute('id');
    mirror.setAttribute('aria-hidden', 'true');
    mirror.tabIndex = -1;
    Object.assign(mirror.style, {
      position: 'absolute',
      visibility: 'hidden',
      pointerEvents: 'none',
      width: `${el.getBoundingClientRect().width}px`,
      height: '0px',
      minHeight: '0px',
      maxHeight: 'none',
      transition: 'none',
      overflow: 'hidden',
      top: '0',
      left: '0',
    });
    mirror.value = el.value + String.fromCharCode(10);
    el.parentElement?.append(mirror);
    const wanted = Math.max(minimum, mirror.scrollHeight);
    mirror.remove();
    const cardBox = card.getBoundingClientRect();
    const bookBox = records.getBoundingClientRect();
    const otherContent = cardBox.height - el.getBoundingClientRect().height;
    const beside = bookBox.left >= cardBox.right - 1;
    const maximum = beside
      ? Math.max(minimum, bookBox.bottom - cardBox.top - otherContent - 8)
      : Math.max(minimum, Math.min(320, innerHeight * 0.45));
    el.style.height = `${Math.ceil(Math.min(wanted, maximum))}px`;
    el.style.overflowY = wanted > maximum ? 'auto' : 'hidden';
  }, [book, composer]);
  // Restore a saved draft once; layout and preference changes must not resize it.
  useLayoutEffect(() => {
    if (input.current?.value) resize();
  }, [resize]);
  return (
    <div className="r-message-field">
      <textarea
        ref={input}
        className="r-message"
        aria-label={label}
        placeholder={label}
        rows={2}
        value={value}
        onChange={(event) => {
          const next = event.currentTarget.value;
          if (Array.from(next).length > 500) return;
          onValue(next);
          resize();
        }}
      />
    </div>
  );
}
