import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Copy, ExternalLink, RefreshCw } from 'lucide-react';
import { useState } from 'react';
import { useApp } from '../app/context';
import { api, dateTime, money, query } from '../lib/api';
import type { Operation, Order } from '../lib/types';
import { Avatar, Button, Modal } from './ui';
export async function checkout(id: string) {
  const v = await api<{ mode: string; url: string; fields?: Record<string, string> }>('order/payment/start', {
    id,
  });
  if (v.mode === 'redirect') {
    location.assign(v.url);
    return;
  }
  const form = document.createElement('form');
  form.method = 'POST';
  form.action = v.url;
  for (const [name, value] of Object.entries(v.fields ?? {})) {
    const input = document.createElement('input');
    input.type = 'hidden';
    input.name = name;
    input.value = value;
    form.append(input);
  }
  document.body.append(form);
  form.submit();
  form.remove();
}
export function OrderDialog({
  id,
  onClose,
  admin = false,
}: {
  id: string | null;
  onClose: () => void;
  admin?: boolean;
}) {
  const { t, report, notify } = useApp();
  const client = useQueryClient();
  const [busy, setBusy] = useState(false),
    [confirmation, setConfirmation] = useState<Operation | null>(null),
    [pollUntil, setPollUntil] = useState(0);
  const detail = useQuery({
    queryKey: ['order-detail', admin, id],
    queryFn: () => api<Order>(`${admin ? 'admin/' : ''}order/detail?${query({ id: id ?? '' })}`),
    enabled: !!id,
    refetchInterval: () => (Date.now() < pollUntil ? 2000 : false),
    retry: false,
  });
  const o = detail.data;
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      report(e);
    } finally {
      setBusy(false);
    }
  };
  const preview = async (action: string) => {
    if (!id) return;
    setConfirmation(await api<Operation>('admin/order/action/preview', { action, ids: [id] }));
  };
  const execute = async () => {
    if (!confirmation) return;
    await api('admin/order/action/execute', { operation_id: confirmation.id });
    const action = confirmation.action;
    setConfirmation(null);
    notify(t('success'));
    await client.invalidateQueries();
    if (action === 'delete') onClose();
    else setPollUntil(Date.now() + 60000);
  };
  const fields = o
    ? [
        [t('orderId'), o.id],
        [t('merchantOrder'), o.merchant_order_no],
        [t('trade'), o.trade_no || '—'],
        [t('created'), dateTime(o.created_at)],
        [t('paidAt'), dateTime(o.paid_at)],
        [t('expires'), dateTime(o.expires_at)],
        ...(admin
          ? [
              [t('ip'), o.ip || '—'],
              [
                t('region'),
                o.location
                  ? [
                      o.location.country,
                      o.location.province,
                      o.location.city,
                      o.location.district,
                      o.location.street,
                      o.location.isp,
                      o.location.asn,
                    ]
                      .filter(Boolean)
                      .join(' · ')
                  : '—',
              ],
              [
                t('source'),
                o.location
                  ? [o.location.source, o.location.version, dateTime(o.location.queried_at)]
                      .filter(Boolean)
                      .join(' · ')
                  : '—',
              ],
              [t('checked'), dateTime(o.last_checked_at)],
              [t('gateway'), t(o.gateway_status)],
              [t('remote'), t(o.remote_close_status)],
            ]
          : []),
      ]
    : [];
  return (
    <>
      <Modal open={!!id} onOpenChange={(v) => !v && onClose()} title={t('details')} wide>
        {detail.isLoading ? (
          <p>{t('loading')}</p>
        ) : detail.error ? (
          <p role="alert">
            {t('error')} <Button onClick={() => void detail.refetch()}>{t('retry')}</Button>
          </p>
        ) : (
          o && (
            <>
              <div className="order-heading">
                <div className="d-person">
                  <Avatar name={o.nickname} src={o.avatar_url} />
                  <strong>{o.nickname}</strong>
                </div>
                <span className={`a-badge ${o.status === 'paid' ? 'a-paid' : 'a-muted'}`}>{t(o.status)}</span>
              </div>
              <div className="order-amount">
                ¥{money(o.amount_fen)}
                <span>{t(o.channel)}</span>
              </div>
              <dl className="detail-grid">
                {fields.map(([label, value]) => (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              </dl>
              <div className="detail-message">
                <span>{t('message').replace('（选填）', '').replace(' (optional)', '')}</span>
                <p>{o.message || t('noMessage')}</p>
              </div>
              {admin && o.jobs?.some((j) => j.kind !== 'geo') && (
                <div className="job-list">
                  {o.jobs
                    .filter((j) => j.kind !== 'geo')
                    .slice(0, 3)
                    .map((j) => (
                      <div key={j.id}>
                        <span>{j.kind === 'close' ? t('closeOrder') : t('reconcile')}</span>
                        <span>
                          {t(j.status)} · {t(j.result || 'ready')}
                        </span>
                      </div>
                    ))}
                </div>
              )}
              <div className="detail-actions">
                <Button
                  className="d-ghost"
                  onClick={() =>
                    void run(async () => {
                      await navigator.clipboard.writeText(o.id);
                      notify(t('copied'));
                    })
                  }
                >
                  <Copy />
                  {t('copy')}
                </Button>
                {admin ? (
                  <>
                    <Button
                      disabled={busy}
                      onClick={() =>
                        void run(async () => {
                          await api('admin/order/reconcile', {
                            id: o.id,
                            idempotency_key: crypto.randomUUID(),
                          });
                          setPollUntil(Date.now() + 60000);
                          notify(t('queued'));
                          await detail.refetch();
                        })
                      }
                    >
                      <RefreshCw />
                      {t('reconcile')}
                    </Button>
                    {o.status === 'pending' && (
                      <Button disabled={busy} onClick={() => void run(() => preview('close'))}>
                        {t('closeOrder')}
                      </Button>
                    )}
                    <Button
                      onClick={() => void run(() => preview(o.hidden_order ? 'show_order' : 'hide_order'))}
                    >
                      {t(o.hidden_order ? 'show_order' : 'hide_order')}
                    </Button>
                    <Button
                      onClick={() =>
                        void run(() => preview(o.hidden_message ? 'show_message' : 'hide_message'))
                      }
                    >
                      {t(o.hidden_message ? 'show_message' : 'hide_message')}
                    </Button>
                    <Button
                      className="is-danger"
                      disabled={busy}
                      onClick={() => void run(() => preview('delete'))}
                    >
                      {t('delete')}
                    </Button>
                  </>
                ) : (
                  o.status === 'pending' &&
                  new Date(o.expires_at).getTime() > Date.now() && (
                    <Button
                      className="d-primary"
                      disabled={busy}
                      onClick={() => void run(() => checkout(o.id))}
                    >
                      <ExternalLink />
                      {t('continue')}
                    </Button>
                  )
                )}
              </div>
            </>
          )
        )}
      </Modal>
      <Modal
        open={!!confirmation}
        onOpenChange={(v) => !v && setConfirmation(null)}
        title={confirmation ? t(confirmation.action === 'close' ? 'closeOrder' : confirmation.action) : ''}
      >
        <p>
          {t(
            confirmation?.action === 'delete'
              ? 'deleteHint'
              : confirmation?.action === 'close'
                ? 'closeHint'
                : 'confirm',
          )}
        </p>
        <div className="dialog-actions">
          <Button onClick={() => setConfirmation(null)}>{t('cancel')}</Button>
          <Button
            disabled={busy}
            className={confirmation?.action === 'delete' ? 'is-destructive' : 'd-primary'}
            onClick={() => void run(execute)}
          >
            {t('confirm')}
          </Button>
        </div>
      </Modal>
    </>
  );
}
