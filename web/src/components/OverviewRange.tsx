import * as Popover from '@radix-ui/react-popover';
import { CalendarRange } from 'lucide-react';
import { useRef, useState } from 'react';
import { useApp } from '../app/context';
import { DateTimeField, SelectField } from './forms';
import { Button, portal } from './ui';

export type OverviewPeriod = { range: string; from?: string; to?: string };
export function OverviewRange({
  value,
  onChange,
}: {
  value: OverviewPeriod;
  onChange: (value: OverviewPeriod) => void;
}) {
  const { t } = useApp();
  const [open, setOpen] = useState(false);
  const customPending = useRef(false);
  const [from, setFrom] = useState(value.from ?? new Date(Date.now() - 30 * 86400000).toISOString());
  const [to, setTo] = useState(value.to ?? new Date().toISOString());
  const valid = !!from && !!to && new Date(from) < new Date(to);
  return (
    <div className="overview-range">
      <SelectField
        label={t('timeRange')}
        value={value.range}
        onValue={(range) => {
          if (range === 'custom') customPending.current = true;
          else onChange({ range });
        }}
        onCloseAutoFocus={(event) => {
          if (customPending.current) {
            customPending.current = false;
            event.preventDefault();
            setOpen(true);
          }
        }}
        options={[
          ['today', t('today')],
          ['yesterday', t('yesterday')],
          ['7d', t('range7d')],
          ['30d', t('range30d')],
          ['90d', t('range90d')],
          ['all', t('allTime')],
          ['custom', t('customRange')],
        ]}
      />
      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Trigger asChild>
          <Button className="d-icon d-ghost" aria-label={t('customRange')}>
            <CalendarRange />
          </Button>
        </Popover.Trigger>
        <Popover.Portal container={portal()}>
          <Popover.Content
            className="d-pop range-pop"
            align="end"
            sideOffset={8}
            collisionPadding={12}
            aria-label={t('customRange')}
          >
            <div className="range-field">
              <span>{t('from')}</span>
              <DateTimeField label={t('from')} value={from} onValue={setFrom} />
            </div>
            <div className="range-field">
              <span>{t('to')}</span>
              <DateTimeField label={t('to')} value={to} onValue={setTo} />
            </div>
            {!valid && <p role="alert">{t('dateRule')}</p>}
            <Button
              className="d-primary"
              disabled={!valid}
              onClick={() => {
                onChange({ range: 'custom', from, to });
                setOpen(false);
              }}
            >
              {t('apply')}
            </Button>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}
