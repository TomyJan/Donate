import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Download, ExternalLink, RefreshCw } from 'lucide-react';
import { type ReactNode, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { api, dateTime, downloadExport, money, query } from '../lib/api';
import type { Operation, Order } from '../lib/types';
import { ActionConfirm } from './ActionConfirm';
import { CopyButton } from './CopyButton';
import { Avatar, Button, Modal } from './ui';
import './OrderDialog.css';

export function orderCopyText(order: Order, t: (key: string) => string) {
  const singleLine = (value: string) => value.replace(/[\r\n]+/g, ' ').trim();
  return [
    t('orderInfoTitle'),
    `${t('accountLabel')}: ${singleLine(order.nickname)} (${order.user_id})`,
    `${t('amountLabel')}: ￥${money(order.amount_fen)}`,
    `${t('paymentMethod')}: ${t(order.channel)}`,
    `${t('orderId')}: ${order.id}`,
    `${t('merchantOrder')}: ${order.merchant_order_no}`,
    `${t('status')}: ${t(order.status)}`,
    `${t('created')}: ${dateTime(order.created_at)}`,
    ...(order.paid_at ? [`${t('paidAt')}: ${dateTime(order.paid_at)}`] : []),
  ].join('\n');
}
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
    [pollUntil, setPollUntil] = useState(0);
  const activeID = useRef(id);
  activeID.current = id;
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
  const preview = async (action: string, orderID: string) => {
    const operation = await api<Operation>('admin/order/action/preview', { action, ids: [orderID] });
    if (operation.action !== action || operation.items.length !== 1 || operation.items[0]?.id !== orderID)
      throw new Error(t('error'));
    return operation;
  };
  const execute = async (confirmation: Operation, orderID: string) => {
    if (activeID.current !== orderID || confirmation.items[0]?.id !== orderID) throw new Error(t('error'));
    setBusy(true);
    try {
      const result = await api<Operation>('admin/order/action/execute', { operation_id: confirmation.id });
      if (result.action === 'export') await downloadExport(result.id);
      notify(t('success'));
      await client.invalidateQueries();
      if (activeID.current === orderID) {
        if (confirmation.action === 'delete') onClose();
        else if (confirmation.action !== 'export') setPollUntil(Date.now() + 60000);
      }
    } finally {
      setBusy(false);
    }
  };
  const confirmAction = (action: string, label: string, icon?: ReactNode) =>
    o && (
      <ActionConfirm
        key={`${o.id}:${action}`}
        scopeKey={`${id}:${action}`}
        title={label}
        description={t(action === 'delete' ? 'deleteHint' : action === 'close' ? 'closeHint' : 'confirm')}
        trigger={
          <Button disabled={busy} className={action === 'delete' ? 'is-danger' : ''}>
            {icon}
            {label}
          </Button>
        }
        prepare={() => preview(action, o.id)}
        expiresAt={(operation) => operation.expires_at}
        onConfirm={(operation) => execute(operation, o.id)}
        danger={action === 'delete'}
      />
    );
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
              [t('userId'), o.user_id || '—'],
              [t('channelTrade'), o.api_trade_no || '—'],
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
              ...(o.location?.latitude != null && o.location.longitude != null
                ? [[t('coordinates'), `${o.location.latitude}, ${o.location.longitude}`]]
                : []),
              ...(o.location?.accuracy_radius != null
                ? [[t('accuracy'), `${o.location.accuracy_radius} km`]]
                : []),
              [t('checked'), dateTime(o.last_checked_at)],
              [t('gateway'), t(o.gateway_status)],
              [t('remote'), t(o.remote_close_status)],
            ]
          : []),
      ]
    : [];
  return (
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
            <div className="order-amount-row">
              <div className="order-amount">
                ¥{money(o.amount_fen)}
                <span>{t(o.channel)}</span>
              </div>
              {!admin && <CopyButton key={o.id} text={orderCopyText(o, t)} label={t('copyInfo')} />}
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
              {admin ? (
                <>
                  <CopyButton key={o.id} text={o.id} label={t('copy')} />
                  {confirmAction('export', t('export'), <Download />)}
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
                  {o.status === 'pending' && confirmAction('close', t('closeOrder'))}
                  {confirmAction(
                    o.hidden_order ? 'show_order' : 'hide_order',
                    t(o.hidden_order ? 'show_order' : 'hide_order'),
                  )}
                  {confirmAction(
                    o.hidden_message ? 'show_message' : 'hide_message',
                    t(o.hidden_message ? 'show_message' : 'hide_message'),
                  )}
                  {confirmAction('delete', t('delete'))}
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
  );
}
