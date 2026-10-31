import { ChevronDown } from 'lucide-react';
import { motion, useReducedMotion } from 'motion/react';
import { useId, useState } from 'react';
import { useApp } from '../app/context';
import { dateTime } from '../lib/api';
import type { Order } from '../lib/types';
import { Button } from './ui';
import './LocationCheck.css';

export function LocationCheck({ check }: { check: NonNullable<Order['location_check']> }) {
  const { t } = useApp();
  const [open, setOpen] = useState(false);
  const panel = useId();
  const reduced = useReducedMotion();
  const statuses = ['single', 'corroborated', 'conflict', 'incomplete', 'unavailable'];
  const status = statuses.includes(check.verification ?? '') ? check.verification : 'unavailable';
  return (
    <section className="geo-check" aria-label={t('geoVerification')}>
      <Button
        className="d-ghost geo-check-toggle"
        aria-expanded={open}
        aria-controls={panel}
        onClick={() => setOpen((value) => !value)}
      >
        <span>{t('geoVerification')}</span>
        <span className={status === 'conflict' ? 'a-badge a-muted' : 'a-badge'}>
          {t(`geoVerification_${status}`)}
        </span>
        <ChevronDown />
      </Button>
      <motion.div
        id={panel}
        initial={false}
        animate={{ height: open ? 'auto' : 0, opacity: open ? 1 : 0 }}
        transition={{ duration: reduced ? 0 : 0.3 }}
        inert={!open}
        aria-hidden={!open}
        className="geo-check-panel"
      >
        <div className="geo-check-content">
          <div className="geo-check-meta">
            <time dateTime={check.queried_at}>{dateTime(check.queried_at)}</time>
            {check.conflict && status !== 'conflict' && <span>{t('geoHadDisagreement')}</span>}
          </div>
          <ul>
            {(check.evidence ?? []).map((item) => (
              <li key={`${item.source}:${item.queried_at}`}>
                <strong>
                  {['xdb', 'mmdb'].includes(item.source) ? item.source.toUpperCase() : item.source}
                </strong>
                <span className="geo-check-region">
                  {[item.country, item.province, item.city].filter(Boolean).join(' · ') || '—'}
                </span>
                <span className="geo-check-outcome">
                  {t(
                    `geoEvidence_${['complete', 'incomplete', 'unavailable'].includes(item.outcome) ? item.outcome : 'unavailable'}`,
                  )}
                </span>
                {item.version && <small>{item.version}</small>}
              </li>
            ))}
          </ul>
        </div>
      </motion.div>
    </section>
  );
}
