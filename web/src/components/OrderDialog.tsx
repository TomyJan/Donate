import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Download, ExternalLink, LocateFixed, RefreshCw } from 'lucide-react';
import { type ReactNode, useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { APIError, api, dateTime, downloadExport, money, query } from '../lib/api';
import { locationResultKey } from '../lib/operation-results';
import { rememberPayment } from '../lib/payment-return';
import type { Operation, Order } from '../lib/types';
import { ActionConfirm } from './ActionConfirm';
import { AdminLoading } from './AdminLoading';
import { CopyButton } from './CopyButton';
import { DialogActions } from './DialogActions';
import { RequestError } from './RequestError';
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
    `${t('trade')}: ${order.trade_no || '—'}`,
    `${t('status')}: ${t(order.status)}`,
    `${t('created')}: ${dateTime(order.created_at)}`,
    ...(order.paid_at ? [`${t('paidAt')}: ${dateTime(order.paid_at)}`] : []),
    `${t('messageColumn')}:\n${order.message || t('noMessage')}`,
  ].join('\n');
}
export async function checkout(id: string) {
  const v = await api<{ mode: string; url: string; fields?: Record<string, string> }>('order/payment/start', {
    id,
  });
  rememberPayment(id);
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
  const { t, report, notify, profile } = useApp();
  const identity = profile ? `${profile.id}:${profile.csrf_token}` : 'guest';
  const client = useQueryClient();
  const [busy, setBusy] = useState(false),
    [pollUntil, setPollUntil] = useState(0);
  const activeID = useRef(id);
  activeID.current = id;
  const detail = useQuery({
    queryKey: ['order-detail', admin, id, identity],
    queryFn: ({ signal }) =>
      api<Order>(`${admin ? 'admin/' : ''}order/detail?${query({ id: id ?? '' })}`, undefined, signal),
    enabled: !!id,
    refetchInterval: () => (Date.now() < pollUntil ? 2000 : false),
    retry: false,
  });
  const unavailable =
    detail.error instanceof APIError &&
    ['NOT_FOUND', 'UNAUTHORIZED', 'FORBIDDEN', 'ACCOUNT_BANNED'].includes(detail.error.code);
  useEffect(() => {
    if (!unavailable) return;
    const cached = client
      .getQueryCache()
      .find({ queryKey: ['order-detail', admin, id, identity], exact: true });
    // Preserve the terminal error while revoking its data for every observer,
    // including dialogs reopened after this component has unmounted.
    if (cached?.state.data !== undefined) cached.setState({ data: undefined, dataUpdatedAt: 0 });
  }, [unavailable, client, admin, id, identity]);
  const o = unavailable ? undefined : detail.data;
  // Background polling does not revoke confirmations; failed refreshes still do.
  const actionsDisabled = busy || !!detail.error;
  const canContinue = !!o && o.status === 'pending' && new Date(o.expires_at).getTime() > Date.now();
  const reconciliations = o?.reconciliations ?? [];
  const manualJobIDs = new Set(reconciliations.map((request) => request.job_id));
  const taskRecords = [
    ...reconciliations.map((request) => ({
      ...request,
      key: `request:${request.id}`,
      label: t('reconcile'),
      displayResult: request.result || 'ready',
    })),
    ...(o?.jobs ?? [])
      .filter((job) => (job.kind !== 'geo' || job.source === 'admin_refresh') && !manualJobIDs.has(job.id))
      .map((job) => ({
        ...job,
        key: `job:${job.id}`,
        label: t(
          job.kind === 'geo' ? 'refresh_location' : job.kind === 'close' ? 'closeOrder' : 'paymentCheck',
        ),
        displayResult: job.kind !== 'geo' ? job.result || 'ready' : locationResultKey(job.result, job.status),
      })),
  ];
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
      notify(
        t(
          result.action === 'refresh_location'
            ? result.items.some((item) => item.result === 'queued')
              ? 'queued'
              : result.items[0]?.result === 'disabled'
                ? 'disabled'
                : 'success'
            : 'success',
        ),
      );
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
          <Button disabled={actionsDisabled} className={action === 'delete' ? 'is-danger' : ''}>
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
        [t('ip'), o.ip || '—'],
        ...(!admin ? [[t('region'), o.province || t('unknownRegion')]] : []),
        ...(admin
          ? [
              [t('userId'), o.user_id || '—'],
              ...(o.qq_number ? [[t('qq'), o.qq_number]] : []),
              [t('channelTrade'), o.api_trade_no || '—'],
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
      {detail.error && (
        <RequestError error={detail.error} busy={detail.isFetching} onRetry={() => void detail.refetch()} />
      )}
      {detail.isLoading ? (
        <AdminLoading kind="detail" />
      ) : (
        o && (
          <div className="order-detail">
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
              {(o.hidden_order || o.hidden_message) && (
                // biome-ignore lint/a11y/useSemanticElements: These are grouped visibility statuses, not form fields.
                <div className="order-visibility" role="group" aria-label={t('visibility')}>
                  {o.hidden_order && <span className="a-badge a-muted">{t('hidden_order')}</span>}
                  {o.hidden_message && <span className="a-badge a-muted">{t('hidden_message')}</span>}
                </div>
              )}
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
            {admin && taskRecords.length > 0 && (
              <div className="job-list">
                {taskRecords.map((record) => (
                  <div key={record.key}>
                    <span>
                      {record.label} <time dateTime={record.created_at}>{dateTime(record.created_at)}</time>
                    </span>
                    <span>
                      {t(record.status)} · {t(record.displayResult)}
                    </span>
                  </div>
                ))}
              </div>
            )}
            {(admin || canContinue) && (
              <DialogActions className="detail-actions">
                {admin ? (
                  <>
                    <CopyButton key={o.id} text={o.id} label={t('copy')} />
                    {confirmAction('export', t('export'), <Download />)}
                    <Button
                      disabled={actionsDisabled || o.protocol === 'legacy'}
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
                    {confirmAction('refresh_location', t('refresh_location'), <LocateFixed />)}
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
                  canContinue && (
                    <Button
                      className="d-primary"
                      disabled={actionsDisabled}
                      onClick={() => void run(() => checkout(o.id))}
                    >
                      <ExternalLink />
                      {t('continue')}
                    </Button>
                  )
                )}
              </DialogActions>
            )}
          </div>
        )
      )}
    </Modal>
  );
}
