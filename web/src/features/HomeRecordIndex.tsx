import { motion } from 'motion/react';
import { useLayoutEffect, useRef } from 'react';
import { useApp } from '../app/context';
import { dateTime, money } from '../lib/api';
import type { Records } from '../lib/types';

type Props = {
  pages: Records[];
  scope: string;
  mine: boolean;
  selected: number;
  reduced: boolean;
  busy: boolean;
  onSelect: (index: number) => void;
};

export function HomeRecordIndex({ pages, scope, mine, selected, reduced, busy, onSelect }: Props) {
  const { t } = useApp();
  const seen = useRef(new Set<string>());
  useLayoutEffect(() => {
    for (const page of pages) for (const record of page.items) seen.current.add(`${scope}:${record.id}`);
  }, [pages, scope]);
  let offset = 0;
  return (
    <div className="r-index" aria-busy={busy}>
      {pages.map((page, pageIndex) => {
        const start = offset;
        offset += page.items.length;
        const expanding =
          pageIndex > 0 && page.items.some((record) => !seen.current.has(`${scope}:${record.id}`));
        return (
          <motion.div
            className="r-index-page"
            key={`${scope}:${page.items[0]?.id ?? pageIndex}`}
            initial={expanding && !reduced ? { height: 0 } : false}
            animate={{ height: 'auto' }}
            transition={{ duration: reduced ? 0 : 0.44, ease: [0.2, 0.75, 0.25, 1] }}
          >
            {page.items.map((record, row) => {
              const index = start + row;
              const entering = expanding && !seen.current.has(`${scope}:${record.id}`);
              return (
                <motion.button
                  type="button"
                  key={record.id}
                  className="r-index-row"
                  aria-pressed={index === selected}
                  onClick={() => onSelect(index)}
                  initial={entering && !reduced ? { opacity: 0, y: 8 } : false}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{
                    duration: reduced ? 0 : 0.28,
                    delay: reduced || !entering ? 0 : Math.min(row * 0.035, 0.14),
                    ease: [0.2, 0.75, 0.25, 1],
                  }}
                >
                  <span className="r-index-label">
                    {mine ? (
                      <time>{dateTime(record.date)}</time>
                    ) : (
                      <>
                        <span>{record.nickname}</span>
                        <time className="r-index-time">{dateTime(record.date)}</time>
                      </>
                    )}
                    {mine && record.status !== 'paid' && (
                      <span className="r-index-status">{t(record.status)}</span>
                    )}
                  </span>
                  <span className="r-index-money">¥{money(record.amount_fen)}</span>
                </motion.button>
              );
            })}
          </motion.div>
        );
      })}
    </div>
  );
}
