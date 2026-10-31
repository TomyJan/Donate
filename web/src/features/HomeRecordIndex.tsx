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
  arrivingId?: string;
  onSelect: (index: number) => void;
};

export function HomeRecordIndex({
  pages,
  scope,
  mine,
  selected,
  reduced,
  busy,
  arrivingId,
  onSelect,
}: Props) {
  const { t } = useApp();
  const seen = useRef(new Set<string>());
  useLayoutEffect(() => {
    for (const page of pages) for (const record of page.items) seen.current.add(`${scope}:${record.id}`);
  }, [pages, scope]);
  let offset = 0;
  return (
    <div className="r-index" aria-busy={busy}>
      {pages.flatMap((page, pageIndex) =>
        page.items.map((record, row) => {
          const index = offset++;
          const entering =
            (pageIndex > 0 || record.id === arrivingId) && !seen.current.has(`${scope}:${record.id}`);
          const delay = reduced || !entering ? 0 : Math.min(row * 0.035, 0.14);
          return (
            <motion.div
              className="r-index-entry"
              key={`${scope}:${record.id}`}
              initial={entering && !reduced ? { height: 0 } : false}
              animate={{ height: 'auto' }}
              transition={{ duration: reduced ? 0 : 0.64, delay, ease: [0.2, 0.75, 0.25, 1] }}
            >
              <motion.button
                type="button"
                className="r-index-row"
                aria-pressed={index === selected}
                onClick={() => onSelect(index)}
                initial={entering && !reduced ? { opacity: 0, y: 8 } : false}
                animate={{ opacity: 1, y: 0 }}
                transition={{
                  duration: reduced ? 0 : 0.4,
                  delay: entering && !reduced ? delay + 0.04 : 0,
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
            </motion.div>
          );
        }),
      )}
    </div>
  );
}
