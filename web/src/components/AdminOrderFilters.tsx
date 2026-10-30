import { Search, SlidersHorizontal } from 'lucide-react';
import { motion, useReducedMotion } from 'motion/react';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { acceptAmountEdit, amountFen } from '../lib/amount';
import { AdminUserFilter } from './AdminUserFilter';
import { ConstrainedInput } from './ConstrainedInput';
import { DateTimeField, SelectField } from './forms';
import { Button } from './ui';
import './AdminOrderFilters.css';

export type AdvancedOrderFilters = {
  from: string;
  to: string;
  minimum: string;
  maximum: string;
  visibility: string;
};
type Props = {
  search: string;
  onSearch: (value: string) => void;
  status: string;
  onStatus: (value: string) => void;
  channel: string;
  onChannel: (value: string) => void;
  user: string;
  onUser: (id: string) => void;
  value: AdvancedOrderFilters;
  onApply: (value: AdvancedOrderFilters) => void;
};
const empty: AdvancedOrderFilters = { from: '', to: '', minimum: '', maximum: '', visibility: '' };

export function AdminOrderFilters({
  search,
  onSearch,
  status,
  onStatus,
  channel,
  onChannel,
  user,
  onUser,
  value,
  onApply,
}: Props) {
  const { t, notify } = useApp();
  const reduced = useReducedMotion();
  const root = useRef<HTMLDivElement>(null);
  const panelId = useId();
  const [width, setWidth] = useState(0);
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(value);
  const { from, to, minimum, maximum, visibility } = value;
  useEffect(() => {
    setDraft({ from, to, minimum, maximum, visibility });
  }, [from, to, minimum, maximum, visibility]);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    const measure = () => setWidth(element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const visible = width >= 760 ? 3 : width >= 600 ? 2 : width >= 460 ? 1 : 0;
  const basic = [
    {
      key: 'status',
      value: status,
      node: (
        <SelectField
          label={t('status')}
          value={status}
          onValue={onStatus}
          options={['', 'paid', 'pending', 'closed'].map((key) => [key, t(key || 'allStatus')])}
        />
      ),
    },
    {
      key: 'channel',
      value: channel,
      node: (
        <SelectField
          label={t('channel')}
          value={channel}
          onValue={onChannel}
          options={['', 'alipay', 'wxpay'].map((key) => [key, t(key || 'allChannels')])}
        />
      ),
    },
    { key: 'user', value: user, node: <AdminUserFilter value={user} onValue={onUser} /> },
  ];
  const tucked = basic.slice(visible);
  const active = tucked.filter((filter) => filter.value).length + Object.values(value).filter(Boolean).length;
  const apply = () => {
    if (
      (draft.from && !Number.isFinite(Date.parse(draft.from))) ||
      (draft.to && !Number.isFinite(Date.parse(draft.to))) ||
      (draft.from && draft.to && new Date(draft.from) >= new Date(draft.to))
    ) {
      notify(t('dateRule'), true);
      return;
    }
    const low = amountFen(draft.minimum),
      high = amountFen(draft.maximum);
    if ((draft.minimum && low === null) || (draft.maximum && high === null)) {
      notify(t('amountRule'), true);
      return;
    }
    if (low !== null && high !== null && low > high) {
      notify(t('amountRangeRule'), true);
      return;
    }
    onApply(draft);
  };
  return (
    <div className="a-order-filters" ref={root}>
      <div className="a-tools a-order-tools">
        <label className="a-search">
          <Search />
          <input
            aria-label={t('search')}
            placeholder={t('search')}
            value={search}
            onChange={(event) => onSearch(event.target.value)}
          />
        </label>
        {basic.slice(0, visible).map((filter) => (
          <div className="a-basic-filter" key={filter.key}>
            {filter.node}
          </div>
        ))}
        <Button
          className="a-ghost a-filter-toggle"
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen((current) => !current)}
        >
          <SlidersHorizontal />
          <span>{t('filters')}</span>
          {active > 0 && <small>{active}</small>}
        </Button>
      </div>
      <motion.div
        id={panelId}
        className="a-filter-wrap"
        initial={false}
        aria-hidden={!open}
        inert={!open}
        animate={{ height: open ? 'auto' : 0, opacity: open ? 1 : 0 }}
        transition={{ duration: reduced ? 0 : 0.24 }}
      >
        <div className="a-filters">
          {tucked.map((filter) => (
            <div className="a-field a-tucked-filter" key={filter.key}>
              <span>{t(filter.key === 'user' ? 'filterUser' : filter.key)}</span>
              {filter.node}
            </div>
          ))}
          {(['from', 'to'] as const).map((key) => (
            <div className="a-field" key={key}>
              <span>{t(key)}</span>
              <DateTimeField
                value={draft[key]}
                label={t(key)}
                onValue={(next) => setDraft((current) => ({ ...current, [key]: next }))}
              />
            </div>
          ))}
          <label className="a-field" htmlFor="filter-minimum">
            <span>{t('minimum')}</span>
            <ConstrainedInput
              id="filter-minimum"
              className="a-input"
              aria-label={t('minimum')}
              inputMode="decimal"
              accept={acceptAmountEdit}
              value={draft.minimum}
              onValue={(next) => setDraft((current) => ({ ...current, minimum: next }))}
            />
          </label>
          <div className="a-filter-pair">
            <label className="a-field" htmlFor="filter-maximum">
              <span>{t('maximum')}</span>
              <ConstrainedInput
                id="filter-maximum"
                className="a-input"
                aria-label={t('maximum')}
                inputMode="decimal"
                accept={acceptAmountEdit}
                value={draft.maximum}
                onValue={(next) => setDraft((current) => ({ ...current, maximum: next }))}
              />
            </label>
            <div className="a-field">
              <span>{t('visibility')}</span>
              <SelectField
                label={t('visibility')}
                value={draft.visibility}
                onValue={(next) => setDraft((current) => ({ ...current, visibility: next }))}
                options={['', 'public', 'hidden_order', 'hidden_message'].map((key) => [
                  key,
                  t(key || 'allVisibility'),
                ])}
              />
            </div>
          </div>
          <div className="a-filter-bottom">
            <Button
              className="d-ghost"
              onClick={() => {
                setDraft(empty);
                onApply(empty);
              }}
            >
              {t('reset')}
            </Button>
            <Button className="d-primary" onClick={apply}>
              {t('apply')}
            </Button>
          </div>
        </div>
      </motion.div>
    </div>
  );
}
