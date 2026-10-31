import { Search, SlidersHorizontal } from 'lucide-react';
import { motion, useReducedMotion } from 'motion/react';
import type { ReactNode } from 'react';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { DateTimeField, SelectField } from '../components/forms';
import { Button } from '../components/ui';
import { taskLabel } from './TaskQueue';

type Filters = { search: string; kind: string; status: string; source: string; from: string; to: string };
export function TaskQueueFilters({
  value,
  onChange,
  retry,
  hasSelection,
  onClearSelection,
}: {
  value: Filters;
  onChange: (value: Partial<Filters>) => void;
  retry: ReactNode;
  hasSelection: boolean;
  onClearSelection: () => void;
}) {
  const { t, notify } = useApp();
  const reduced = useReducedMotion();
  const root = useRef<HTMLDivElement>(null);
  const panelId = useId();
  const [width, setWidth] = useState(0);
  const [open, setOpen] = useState(false);
  const [dates, setDates] = useState({ from: value.from, to: value.to });
  useEffect(() => {
    setDates({ from: value.from, to: value.to });
  }, [value.from, value.to]);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    const measure = () => setWidth(element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const visible = width >= 920 ? 3 : width >= 780 ? 2 : width >= 640 ? 1 : 0;
  const basic = [
    { key: 'kind' as const, label: 'taskKind', all: 'taskAllKinds', options: ['payment', 'geo', 'close'] },
    {
      key: 'status' as const,
      label: 'status',
      all: 'allStatus',
      options: ['ready', 'running', 'done', 'failed'],
    },
    {
      key: 'source' as const,
      label: 'taskSource',
      all: 'taskAllSources',
      options: [
        'automatic',
        'manual',
        'browser_return',
        'admin',
        'admin_retry',
        'admin_refresh',
        'close_followup',
      ],
    },
  ];
  const select = (filter: (typeof basic)[number]) => (
    <SelectField
      label={t(filter.label)}
      value={value[filter.key]}
      onValue={(next) => onChange({ [filter.key]: next })}
      options={['', ...filter.options].map((key) => [
        key,
        key ? taskLabel(t, filter.key, key) : t(filter.all),
      ])}
    />
  );
  const tucked = basic.slice(visible);
  const active =
    tucked.filter((filter) => value[filter.key]).length + Number(!!value.from) + Number(!!value.to);
  const applyDates = () => {
    if (
      (dates.from && !Number.isFinite(Date.parse(dates.from))) ||
      (dates.to && !Number.isFinite(Date.parse(dates.to))) ||
      (dates.from && dates.to && Date.parse(dates.from) >= Date.parse(dates.to))
    ) {
      notify(t('dateRule'), true);
      return;
    }
    onChange(dates);
  };
  return (
    <div className="task-filters" ref={root} data-compact={width < 500}>
      <div className="a-tools task-tools">
        <label className="a-search">
          <Search />
          <input
            aria-label={t('taskSearch')}
            placeholder={t('taskSearch')}
            value={value.search}
            onChange={(event) => onChange({ search: event.target.value })}
          />
        </label>
        <div className="task-batch-slot">{retry}</div>
        {basic.slice(0, visible).map((filter) => (
          <div className="task-basic-filter" key={filter.key}>
            {select(filter)}
          </div>
        ))}
        <Button
          className="d-ghost task-filter-toggle"
          aria-label={t('taskMoreFilters')}
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen((current) => !current)}
        >
          <SlidersHorizontal />
          <span>{t('taskMoreFilters')}</span>
          {active > 0 && <small aria-hidden="true">{active}</small>}
        </Button>
      </div>
      <motion.div
        id={panelId}
        className="task-filter-wrap"
        initial={false}
        aria-hidden={!open}
        inert={!open}
        animate={{ height: open ? 'auto' : 0, opacity: open ? 1 : 0 }}
        transition={{ duration: reduced ? 0 : 0.24 }}
      >
        <div className="task-filter-panel">
          {tucked.map((filter) => (
            <div className="task-filter-field" key={filter.key}>
              <span>{t(filter.label)}</span>
              {select(filter)}
            </div>
          ))}
          {(['from', 'to'] as const).map((key) => (
            <div className="task-filter-field task-filter-date" key={key}>
              <span>
                {t('taskCreatedRange')} · {t(key)}
              </span>
              <DateTimeField
                value={dates[key]}
                label={t(key)}
                onValue={(next) => setDates((current) => ({ ...current, [key]: next }))}
              />
            </div>
          ))}
          <div className="task-filter-actions">
            <Button
              className="d-ghost"
              onClick={() => {
                setDates({ from: '', to: '' });
                onChange({ kind: '', status: '', source: '', from: '', to: '' });
              }}
            >
              {t('reset')}
            </Button>
            {hasSelection && (
              <Button className="d-ghost" onClick={onClearSelection}>
                {t('clear')}
              </Button>
            )}
            <Button className="d-primary" onClick={applyDates}>
              {t('apply')}
            </Button>
          </div>
        </div>
      </motion.div>
    </div>
  );
}
