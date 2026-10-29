import { useInfiniteQuery } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, ChevronDown } from 'lucide-react';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { ConstrainedInput } from '../components/ConstrainedInput';
import { MessageInput } from '../components/MessageInput';
import { checkout, OrderDialog } from '../components/OrderDialog';
import { Avatar, Button, Empty, Menu } from '../components/ui';
import { acceptAmountEdit, amountFen } from '../lib/amount';
import { api, dateTime, money, query } from '../lib/api';
import type { Order, Records } from '../lib/types';

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
    [selected, setSelected] = useState(0),
    [detail, setDetail] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    [open, setOpen] = useState(() => innerWidth > 680),
    [height, setHeight] = useState(180);
  const requestKey = useRef(crypto.randomUUID());
  const composer = useRef<HTMLElement>(null),
    book = useRef<HTMLElement>(null),
    receipt = useRef<HTMLElement>(null);
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
  // biome-ignore lint/correctness/useExhaustiveDependencies: remeasure when rendered receipt content or textarea changes.
  useLayoutEffect(() => {
    const el = receipt.current;
    if (!el) return;
    const measure = () => setHeight(el.offsetHeight);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [current?.id, current?.message, app.lang, mine]);
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
    setSelected(0);
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
              onClick={() => setSelected((v) => v - 1)}
            >
              <ArrowLeft />
            </Button>
            <Button
              className="d-icon d-ghost"
              aria-label={t('next')}
              disabled={selected >= records.length - 1}
              onClick={() => setSelected((v) => v + 1)}
            >
              <ArrowRight />
            </Button>
          </div>
        </div>
        {current ? (
          <motion.div className="r-stack" animate={{ height }} transition={{ duration: reduced ? 0 : 0.28 }}>
            <AnimatePresence initial={false}>
              {
                <motion.article
                  key={current.id}
                  ref={receipt}
                  className="r-ticket r-featured"
                  initial={{ opacity: 0, x: reduced ? 0 : 15, rotate: reduced ? 0 : 0.6 }}
                  animate={{ opacity: 1, x: 0, rotate: 0 }}
                  exit={{ opacity: 0, x: reduced ? 0 : -18, rotate: reduced ? 0 : -0.6 }}
                  transition={{ duration: reduced ? 0 : 0.24 }}
                >
                  <div className="r-person">
                    <Avatar name={current.nickname} src={current.avatar_url} />
                    <strong>{current.nickname}</strong>
                    {mine && current.status !== 'paid' && (
                      <span className="r-status">{t(current.status)}</span>
                    )}
                  </div>
                  <div className="r-receipt-value">
                    <div
                      className="r-receipt-amount"
                      style={{
                        fontSize: `${Math.min(64, Math.floor((mine ? 220 : 360) / (money(current.amount_fen).length + 1)))}px`,
                      }}
                    >
                      ¥{money(current.amount_fen)}
                    </div>
                    {mine && (
                      <div className="r-receipt-action">
                        <Button className="d-ghost" onClick={() => setDetail(current.id)}>
                          {t('details')}
                        </Button>
                        {current.status === 'pending' && (
                          <Button
                            className="d-primary"
                            onClick={() => void checkout(current.id).catch(report)}
                          >
                            {t('continue')}
                          </Button>
                        )}
                      </div>
                    )}
                  </div>
                  {current.message && <p className="r-note">{current.message}</p>}
                  <div className="r-meta">
                    <span>{current.province}</span>
                    <time>{dateTime(current.date)}</time>
                  </div>
                </motion.article>
              }
            </AnimatePresence>
          </motion.div>
        ) : (
          <Empty text={list.isLoading ? t('loading') : list.error ? t('unavailable') : t('empty')} />
        )}
        <div className="r-index">
          {records.map((record, index) => (
            <button
              type="button"
              key={record.id}
              className="r-index-row"
              aria-pressed={index === selected}
              onClick={() => setSelected(index)}
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
            </button>
          ))}
        </div>
        <div className="r-more">
          <Button
            className="d-ghost"
            disabled={!!profile && !list.hasNextPage && !list.error}
            onClick={() => {
              if (!profile) {
                app.setLoginOpen(true);
                return;
              }
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
