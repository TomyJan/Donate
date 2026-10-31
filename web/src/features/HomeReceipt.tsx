import { motion, useIsPresent, usePresenceData } from 'motion/react';
import { useLayoutEffect, useRef } from 'react';
import { useApp } from '../app/context';
import { useRequestScope } from '../app/useRequestScope';
import { checkout } from '../components/OrderDialog';
import { Avatar, Button } from '../components/ui';
import { dateTime, money } from '../lib/api';
import type { RecordItem } from '../lib/types';

type Turn = { direction: number; previousId: string | null };
type Props = {
  record: RecordItem;
  reduced: boolean;
  onHeight: (height: number) => void;
  onDetail: (id: string) => void;
};

export function HomeReceipt({ record, reduced, onHeight, onDetail }: Props) {
  const { lang } = useApp();
  const present = useIsPresent();
  const turn = usePresenceData() as Turn;
  const paper = useRef<HTMLElement>(null);
  // Only the current paper owns the stack height; an outgoing resize cannot restore an old size.
  useLayoutEffect(() => {
    const el = paper.current;
    if (!el || !present) return;
    const measure = () => onHeight(el.offsetHeight);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [present, onHeight]);
  // Content and locale changes are also observed through the paper's intrinsic height.
  return (
    <motion.article
      ref={paper}
      className={`r-ticket r-featured${present ? '' : ' r-leaving'}`}
      aria-hidden={present ? undefined : true}
      inert={!present}
      style={{
        zIndex: present ? 1 : 2,
        // On an interrupted turn, retire earlier exits immediately instead of exposing them again.
        visibility: !present && turn.previousId !== record.id ? 'hidden' : undefined,
      }}
      initial={
        reduced
          ? false
          : {
              opacity: 0.5,
              y: 12,
              rotateX: -12,
              rotateY: -turn.direction * 14,
              rotateZ: 2 - turn.direction * 5,
            }
      }
      animate={{ opacity: 1, x: 0, y: 0, rotateX: 0, rotateY: 0, rotateZ: 2 }}
      exit="turned"
      variants={{
        turned: (exit: Turn) => ({
          opacity: 0,
          x: reduced ? 0 : exit.direction * 22,
          y: reduced ? 0 : -28,
          rotateX: reduced ? 0 : 24,
          rotateY: reduced ? 0 : exit.direction * 30,
          rotateZ: reduced ? 2 : 2 + exit.direction * 8,
          transition: { duration: reduced ? 0 : 0.28, ease: [0.2, 0.75, 0.25, 1] },
        }),
      }}
      transition={{ duration: reduced ? 0 : 0.46, ease: [0.2, 0.75, 0.25, 1] }}
      lang={lang}
    >
      <ReceiptBody record={record} onDetail={onDetail} />
    </motion.article>
  );
}

export function ReceiptBody({ record, onDetail }: Pick<Props, 'record' | 'onDetail'>) {
  const { t, report } = useApp();
  const owned = record.is_mine;
  const { isCurrent } = useRequestScope(owned && record.status === 'pending' ? record.id : null);
  const digits = money(record.amount_fen).length + 1;
  return (
    <>
      <div className="r-person">
        <Avatar name={record.nickname} src={record.avatar_url} />
        <strong>{record.nickname}</strong>
        {owned && record.status !== 'paid' && <span className="r-status">{t(record.status)}</span>}
      </div>
      <div className="r-receipt-value">
        <div className="r-receipt-total">
          <div
            className="r-receipt-amount"
            style={{
              fontSize: `min(${Math.min(64, Math.floor(360 / digits))}px, calc(100cqi / ${digits} / .56))`,
            }}
          >
            ¥{money(record.amount_fen)}
          </div>
        </div>
        {owned && (
          <div className="r-receipt-action">
            <Button className="d-ghost" onClick={() => onDetail(record.id)}>
              {t('details')}
            </Button>
            {record.status === 'pending' && (
              <Button
                className="d-primary"
                onClick={() =>
                  void checkout(record.id, isCurrent).catch((error) => {
                    if (isCurrent()) report(error);
                  })
                }
              >
                {t('continue')}
              </Button>
            )}
          </div>
        )}
      </div>
      {record.message && <p className="r-note">{record.message}</p>}
      <div className="r-meta">
        <span className="r-province">{record.province || t('unknownRegion')}</span>
        <time>{dateTime(record.date)}</time>
      </div>
    </>
  );
}
