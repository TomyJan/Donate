import { useInfiniteQuery } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, ChevronDown } from 'lucide-react';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { ConstrainedInput } from '../components/ConstrainedInput';
import { MessageInput } from '../components/MessageInput';
import { checkout, OrderDialog } from '../components/OrderDialog';
import { Button, Empty, Menu } from '../components/ui';
import { acceptAmountEdit, amountFen } from '../lib/amount';
import { api, query } from '../lib/api';
import type { Order, Records } from '../lib/types';
import { HomeReceipt } from './HomeReceipt';
import { HomeRecordIndex } from './HomeRecordIndex';

type Draft = { amount: string; message: string; channel: string };
function initialDraft(): Draft {
  try {
    const v = JSON.parse(sessionStorage.getItem('donate-draft') ?? 'null');
    if (
      v &&
      typeof v.amount === 'string' &&
      acceptAmountEdit(v.amount) &&
      typeof v.message === 'string' &&
      ['alipay', 'wxpay'].includes(v.channel)
    )
      return v;
  } catch {}
  return { amount: '20.26', message: '', channel: 'alipay' };
}
export function Home() {
  const app = useApp(),
    { t, profile, report } = app;
  const [draft, setDraft] = useState(initialDraft),
    [mine, setMine] = useState(false),
    [{ selected, direction, previousId }, setSelection] = useState({
      selected: 0,
      direction: 1,
      previousId: null as string | null,
    }),
    [detail, setDetail] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    [open, setOpen] = useState(() => innerWidth > 680),
    [height, setHeight] = useState(180);
  const requestKey = useRef(crypto.randomUUID());
  const composer = useRef<HTMLElement>(null),
    book = useRef<HTMLElement>(null);
  const reduced = useReducedMotion();
  const list = useInfiniteQuery({
    queryKey: ['records', profile?.id ?? 'guest', mine],
    queryFn: ({ pageParam }) =>
      api<Records>(`order/list?${query({ only_mine: mine ? 'true' : '', cursor: pageParam })}`),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const records = list.data?.pages.flatMap((v) => v.items) ?? [];
  const current = records[Math.min(selected, Math.max(0, records.length - 1))];
  useEffect(() => {
    sessionStorage.setItem('donate-draft', JSON.stringify(draft));
    requestKey.current = crypto.randomUUID();
  }, [draft]);
  useEffect(() => {
    if (!profile) setMine(false);
  }, [profile]);
  useEffect(() => {
    if (new URLSearchParams(location.search).get('payment') === 'returned') {
      app.notify(t('checkPayment'));
      history.replaceState(null, '', '/');
      setMine(!!profile);
    }
  }, [app.notify, profile, t]);
  const selectRecord = (index: number) => {
    setSelection((previous) => ({
      selected: index,
      direction: index >= previous.selected ? 1 : -1,
      previousId: records[Math.min(previous.selected, records.length - 1)]?.id ?? null,
    }));
  };
  const pay = async () => {
    if (!profile) {
      app.setLoginOpen(true);
      return;
    }
    const fen = amountFen(draft.amount);
    if (fen === null || busy) return;
    setBusy(true);
    try {
      const order = await api<Order>('order/create', {
        amount_fen: fen,
        channel: draft.channel,
        message: draft.message,
        idempotency_key: requestKey.current,
      });
      await checkout(order.id);
    } catch (e) {
      report(e);
      await list.refetch();
    } finally {
      setBusy(false);
    }
  };
  const chooseMine = () => {
    if (!profile) {
      app.setLoginOpen(true);
      return;
    }
    setSelection({ selected: 0, direction: 1, previousId: null });
    setMine((v) => !v);
  };
  const slip = (
    <article className="r-ticket" ref={composer}>
      <div className="r-compose-title">
        <h2>{t('donate')}</h2>
      </div>
      <label className="r-money" htmlFor="donation-amount">
        <span>¥</span>
        <ConstrainedInput
          id="donation-amount"
          aria-label={t('amount')}
          inputMode="decimal"
          autoComplete="off"
          value={draft.amount}
          accept={acceptAmountEdit}
          onValue={(amount) => setDraft((v) => ({ ...v, amount }))}
          style={{
            fontSize: draft.amount.length > 8 ? '34px' : draft.amount.length > 6 ? '42px' : undefined,
          }}
        />
      </label>
      <div className="r-presets">
        {['0.01', '8.88', '20.26', '50', '128', '256', '648'].map((value) => (
          <button
            type="button"
            className="r-preset"
            key={value}
            aria-pressed={amountFen(draft.amount) === amountFen(value)}
            onClick={() => setDraft((v) => ({ ...v, amount: value }))}
          >
            {value}
          </button>
        ))}
      </div>
      <MessageInput
        value={draft.message}
        label={t('message')}
        onValue={(message) => setDraft((v) => ({ ...v, message }))}
        composer={composer}
        book={book}
      />
      <div className="r-pay">
        <Button
          className="r-btn r-primary"
          disabled={busy || amountFen(draft.amount) === null || !app.site?.payment_enabled}
          onClick={() => void pay()}
        >
          {busy ? t('loading') : `${t('donate')} · ${t(draft.channel)}`}
        </Button>
        <Menu
          label={t('channel')}
          trigger={
            <Button className="r-icon r-channel" aria-label={t('channel')}>
              <ChevronDown />
            </Button>
          }
          items={['alipay', 'wxpay'].map((channel) => ({
            label: t(channel),
            selected: draft.channel === channel,
            action: () => setDraft((v) => ({ ...v, channel })),
          }))}
        />
      </div>
      <p className="r-ticket-foot">{t(app.site?.payment_enabled ? 'foot' : 'paymentDisabled')}</p>
    </article>
  );
  return (
    <main className="r-main">
      <section className={`r-write ${open ? 'is-open' : ''}`}>
        <Button className="mobile-compose-toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          {t('donate')}
          <ChevronDown />
        </Button>
        <div className="composer-body" hidden={!open && innerWidth <= 680}>
          {slip}
        </div>
      </section>
      <section className="r-book" ref={book} aria-label={t('records')}>
        <div className="r-book-head">
          <div className="r-book-title">
            <h2>{t('records')}</h2>
            <Button className="r-ghost r-filter" aria-pressed={mine} onClick={chooseMine}>
              {t('mine')}
            </Button>
          </div>
          <div className="r-page-nav">
            <Button
              className="d-icon d-ghost"
              aria-label={t('previous')}
              disabled={selected <= 0}
              onClick={() => selectRecord(selected - 1)}
            >
              <ArrowLeft />
            </Button>
            <Button
              className="d-icon d-ghost"
              aria-label={t('next')}
              disabled={selected >= records.length - 1}
              onClick={() => selectRecord(selected + 1)}
            >
              <ArrowRight />
            </Button>
          </div>
        </div>
        {current ? (
          <motion.div
            className="r-stack"
            animate={{ height }}
            transition={{ duration: reduced ? 0 : 0.42, ease: [0.2, 0.75, 0.25, 1] }}
          >
            <AnimatePresence initial={false} custom={{ direction, previousId }}>
              <HomeReceipt
                key={current.id}
                record={current}
                reduced={!!reduced}
                onHeight={setHeight}
                onDetail={setDetail}
              />
            </AnimatePresence>
          </motion.div>
        ) : (
          <Empty text={list.isLoading ? t('loading') : list.error ? t('unavailable') : t('empty')} />
        )}
        <HomeRecordIndex
          pages={list.data?.pages ?? []}
          scope={`${profile?.id ?? 'guest'}:${mine}`}
          mine={mine}
          selected={selected}
          reduced={!!reduced}
          busy={list.isFetching}
          onSelect={selectRecord}
        />
        <div className="r-more">
          <Button
            className="d-ghost"
            disabled={list.isFetching || (!!profile && !list.hasNextPage && !list.error)}
            onClick={() => {
              if (!profile) {
                app.setLoginOpen(true);
                return;
              }
              if (list.isFetching) return;
              if (list.error) void list.refetch();
              else void list.fetchNextPage().catch(report);
            }}
          >
            {!profile
              ? t('guestMore')
              : list.error
                ? t('retry')
                : list.isFetchingNextPage
                  ? t('loading')
                  : list.hasNextPage
                    ? t('more')
                    : t('end')}
          </Button>
        </div>
      </section>
      <OrderDialog id={detail} onClose={() => setDetail(null)} />
    </main>
  );
}
