import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight, Check, ChevronDown, LoaderCircle } from 'lucide-react';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { ConstrainedInput } from '../components/ConstrainedInput';
import { MessageInput } from '../components/MessageInput';
import { checkout, OrderDialog } from '../components/OrderDialog';
import { RequestError } from '../components/RequestError';
import { SelectionButton, SelectionGroup } from '../components/SelectionGroup';
import { Button, Empty, Menu } from '../components/ui';
import { acceptAmountEdit, amountFen, yearAmount } from '../lib/amount';
import { api, money, query } from '../lib/api';
import { errorMessage } from '../lib/errors';
import type { Order, RecordItem, Records } from '../lib/types';
import { HomeReceipt, ReceiptBody } from './HomeReceipt';
import { HomeRecordIndex } from './HomeRecordIndex';
import { PaymentReturnStatus } from './PaymentReturnStatus';
import { usePaymentReturn } from './usePaymentReturn';
import { usePaymentTransfer } from './usePaymentTransfer';
import './payment-return.css';

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
  return { amount: yearAmount(), message: '', channel: 'alipay' };
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
    [height, setHeight] = useState(180);
  const requestKey = useRef(crypto.randomUUID());
  const composer = useRef<HTMLElement>(null),
    book = useRef<HTMLElement>(null);
  const reduced = useReducedMotion();
  const payment = usePaymentReturn();
  const client = useQueryClient();
  const [composerEpoch, setComposerEpoch] = useState(0);
  const [delivered, setDelivered] = useState<{
    owner: string;
    mine: boolean;
    item: RecordItem;
    pending: boolean;
    refreshVersion: number;
  } | null>(null);
  const destination = useRef<HTMLDivElement>(null);
  const arrival = useRef<HTMLElement>(null);
  const incoming: RecordItem | null = payment.order
    ? {
        id: payment.order.id,
        is_mine: true,
        nickname: payment.order.nickname,
        avatar_url: payment.order.avatar_url,
        amount_fen: payment.order.amount_fen,
        message: payment.order.message,
        province: payment.order.province,
        status: payment.order.status,
        date: payment.order.paid_at ?? payment.order.created_at,
      }
    : null;
  const list = useInfiniteQuery({
    queryKey: ['records', profile?.id ?? 'guest', mine],
    queryFn: ({ pageParam }) =>
      api<Records>(`order/list?${query({ only_mine: mine ? 'true' : '', cursor: pageParam })}`),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: !app.loading,
    refetchOnWindowFocus: !payment.locked,
    refetchOnReconnect: !payment.locked,
  });
  const finishTransfer = () => {
    const order = payment.order;
    if (order && order.user_id === profile?.id) {
      const queryKey = ['records', profile.id, mine] as const;
      setDelivered({
        owner: profile.id,
        mine,
        pending: true,
        refreshVersion: client.getQueryState(queryKey)?.dataUpdateCount ?? 0,
        item: {
          id: order.id,
          is_mine: true,
          nickname: order.nickname,
          avatar_url: order.avatar_url,
          amount_fen: order.amount_fen,
          message: order.message,
          province: order.province,
          status: order.status,
          date: order.paid_at ?? order.created_at,
        },
      });
      setSelection({ selected: 0, direction: 1, previousId: null });
      setDraft({ amount: yearAmount(), message: '', channel: 'alipay' });
      setComposerEpoch((value) => value + 1);
      // With no cached data, refetch otherwise reuses the initial in-flight
      // request. Its pre-landing result cannot acknowledge this paid snapshot.
      void client
        .cancelQueries({ queryKey, exact: true })
        .then(() => client.refetchQueries({ queryKey, exact: true, type: 'active' }));
    }
    payment.finish();
  };
  usePaymentTransfer({
    active: payment.phase === 'filing',
    reduced: !!reduced,
    composer,
    destination,
    arrival,
    onComplete: finishTransfer,
  });
  // A snapshot only bridges filing and its first successful list refresh. Keep
  // the arrival identity stable so acknowledging the API does not remount the paper.
  useEffect(() => {
    if (delivered?.pending && (delivered.owner !== profile?.id || delivered.mine !== mine)) {
      setDelivered({ ...delivered, pending: false });
    }
  }, [delivered, profile?.id, mine]);
  useEffect(
    () =>
      client.getQueryCache().subscribe((event) => {
        // Cached success survives paused/cancelled requests. A real full-list
        // success is authoritative, even with identical data in the same millisecond.
        if (event.type !== 'updated' || event.action.type !== 'success' || event.action.manual) return;
        const { queryKey, state } = event.query;
        if (queryKey[0] !== 'records' || state.fetchMeta?.fetchMore) return;
        setDelivered((previous) =>
          previous?.pending &&
          previous.owner === queryKey[1] &&
          previous.mine === queryKey[2] &&
          state.dataUpdateCount > previous.refreshVersion
            ? { ...previous, pending: false }
            : previous,
        );
      }),
    [client],
  );
  // A callback can arrive after the public API already includes this payment. Keep that
  // row out until the paper has landed, including the initial response and focus refetches.
  const fetchedPages = (list.data?.pages ?? []).map((page) =>
    payment.locked ? { ...page, items: page.items.filter((item) => item.id !== payment.id) } : page,
  );
  const received =
    delivered && delivered.owner === profile?.id && delivered.mine === mine
      ? delivered.pending
        ? delivered.item
        : (fetchedPages.flatMap((page) => page.items).find((item) => item.id === delivered.item.id) ?? null)
      : null;
  const pages = received
    ? [
        {
          items: [received, ...(fetchedPages[0]?.items ?? []).filter((item) => item.id !== received.id)],
          next_cursor: fetchedPages[0]?.next_cursor ?? '',
        },
        ...fetchedPages
          .slice(1)
          .map((page) => ({ ...page, items: page.items.filter((item) => item.id !== received.id) })),
      ]
    : fetchedPages;
  const records = pages.flatMap((v) => v.items);
  const current = records[Math.min(selected, Math.max(0, records.length - 1))];
  const shownDraft =
    payment.locked && payment.order
      ? {
          amount: money(payment.order.amount_fen),
          channel: payment.order.channel,
          message: payment.order.message,
        }
      : draft;
  useEffect(() => {
    try {
      sessionStorage.setItem('donate-draft', JSON.stringify(draft));
    } catch {
      // Draft editing still works if storage is blocked or full.
    }
    requestKey.current = crypto.randomUUID();
  }, [draft]);
  useEffect(() => {
    if (!profile) setMine(false);
  }, [profile]);
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
    if (fen === null || busy || payment.locked) return;
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
    setDelivered((previous) => (previous?.pending ? { ...previous, pending: false } : previous));
    setSelection({ selected: 0, direction: 1, previousId: null });
    setMine((v) => !v);
  };
  const slip = (
    <motion.article
      className="r-ticket"
      ref={composer}
      key={composerEpoch}
      data-payment-phase={payment.phase}
      aria-busy={payment.phase === 'checking'}
      initial={composerEpoch > 0 && !reduced ? { opacity: 0, y: 18, rotateX: -6 } : false}
      animate={{ opacity: 1, y: 0, rotateX: 0 }}
      transition={{ duration: reduced ? 0 : 0.8, ease: [0.2, 0.75, 0.25, 1] }}
    >
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
          disabled={payment.locked}
          value={shownDraft.amount}
          accept={acceptAmountEdit}
          onValue={(amount) => setDraft((v) => ({ ...v, amount }))}
          style={{
            fontSize:
              shownDraft.amount.length > 8 ? '34px' : shownDraft.amount.length > 6 ? '42px' : undefined,
          }}
        />
      </label>
      <SelectionGroup className="r-presets">
        {['0.01', '8.88', yearAmount(), '50', '128', '256', '648'].map((value) => (
          <SelectionButton
            className="r-preset"
            key={value}
            selected={amountFen(shownDraft.amount) === amountFen(value)}
            disabled={payment.locked}
            aria-pressed={amountFen(shownDraft.amount) === amountFen(value)}
            onClick={() => setDraft((v) => ({ ...v, amount: value }))}
          >
            {value}
          </SelectionButton>
        ))}
      </SelectionGroup>
      <MessageInput
        value={shownDraft.message}
        label={t('message')}
        onValue={(message) => setDraft((v) => ({ ...v, message }))}
        composer={composer}
        book={book}
        disabled={payment.locked}
      />
      <div className="r-pay">
        <Button
          className="r-btn r-primary"
          disabled={
            payment.locked || busy || amountFen(shownDraft.amount) === null || !app.site?.payment_enabled
          }
          onClick={() => void pay()}
        >
          {payment.locked ? (
            <>
              {payment.phase === 'confirmed' || payment.phase === 'filing' ? (
                <Check aria-hidden="true" />
              ) : payment.phase === 'checking' ? (
                <LoaderCircle className="payment-spinner" aria-hidden="true" />
              ) : null}
              {t(
                payment.phase === 'confirmed'
                  ? 'paymentConfirmed'
                  : payment.phase === 'filing'
                    ? 'paymentFiling'
                    : payment.phase === 'login'
                      ? 'paymentSignIn'
                      : payment.phase === 'checking'
                        ? 'paymentChecking'
                        : 'paymentUnconfirmed',
              )}
            </>
          ) : busy ? (
            t('loading')
          ) : (
            `${t('donate')} · ${t(shownDraft.channel)}`
          )}
        </Button>
        <Menu
          label={t('channel')}
          trigger={
            <Button className="r-icon r-channel" aria-label={t('channel')} disabled={payment.locked}>
              <ChevronDown />
            </Button>
          }
          items={['alipay', 'wxpay'].map((channel) => ({
            label: t(channel),
            selected: shownDraft.channel === channel,
            action: () => setDraft((v) => ({ ...v, channel })),
          }))}
        />
      </div>
      {payment.locked ? (
        <PaymentReturnStatus payment={payment} />
      ) : (
        <p className="r-ticket-foot">
          {app.siteError
            ? errorMessage(app.siteError, t)
            : !app.site
              ? t('loading')
              : t(app.site.payment_enabled ? 'foot' : 'paymentDisabled')}
        </p>
      )}
    </motion.article>
  );
  return (
    <main className="r-main">
      <section className="r-write">
        <div className="composer-body">{slip}</div>
      </section>
      <section className="r-book" ref={book} aria-label={t('records')}>
        <div className="r-book-head">
          <div className="r-book-title">
            <h2>{t('records')}</h2>
            <Button
              className="r-ghost r-filter"
              aria-pressed={mine}
              disabled={payment.phase === 'filing'}
              onClick={chooseMine}
            >
              {t('mine')}
            </Button>
          </div>
          <div className="r-page-nav">
            <Button
              className="d-icon d-ghost"
              aria-label={t('previous')}
              disabled={selected <= 0}
              aria-disabled={selected <= 0 || payment.phase === 'filing'}
              onClick={() => {
                if (payment.phase !== 'filing') selectRecord(selected - 1);
              }}
            >
              <ArrowLeft />
            </Button>
            <Button
              className="d-icon d-ghost"
              aria-label={t('next')}
              disabled={selected >= records.length - 1}
              aria-disabled={selected >= records.length - 1 || payment.phase === 'filing'}
              onClick={() => {
                if (payment.phase !== 'filing') selectRecord(selected + 1);
              }}
            >
              <ArrowRight />
            </Button>
          </div>
        </div>
        <div className="receipt-destination" ref={destination} data-receiving={payment.phase === 'filing'}>
          {current ? (
            <motion.div
              className="r-stack"
              animate={{ height }}
              transition={{ duration: reduced ? 0 : 0.42, ease: [0.2, 0.75, 0.25, 1] }}
            >
              <AnimatePresence
                key={delivered?.item.id ?? 'initial'}
                initial={false}
                custom={{ direction, previousId }}
              >
                <HomeReceipt
                  key={current.id}
                  record={current}
                  reduced={!!reduced}
                  onHeight={setHeight}
                  onDetail={setDetail}
                />
              </AnimatePresence>
            </motion.div>
          ) : list.error ? (
            <RequestError error={list.error} busy={list.isFetching} onRetry={() => void list.refetch()} />
          ) : (
            <Empty text={list.isLoading ? t('loading') : t('empty')} />
          )}
          {incoming && (payment.phase === 'confirmed' || payment.phase === 'filing') && (
            <div className="r-stack payment-arrival-stack" aria-hidden="true" inert>
              <article className="r-ticket r-featured payment-arrival" ref={arrival}>
                <ReceiptBody record={incoming} onDetail={() => {}} />
              </article>
            </div>
          )}
        </div>
        <HomeRecordIndex
          pages={pages}
          scope={`${profile?.id ?? 'guest'}:${mine}`}
          mine={mine}
          selected={selected}
          reduced={!!reduced}
          busy={list.isFetching}
          onSelect={(index) => {
            if (payment.phase !== 'filing') selectRecord(index);
          }}
        />
        <div className="r-more">
          <Button
            className="d-ghost"
            disabled={
              payment.phase === 'filing' || list.isFetching || (!!profile && !list.hasNextPage && !list.error)
            }
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
