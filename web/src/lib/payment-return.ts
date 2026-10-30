const pendingKey = 'donate-pending-payment';
const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function rememberPayment(id: string, resumeLogin = false) {
  try {
    sessionStorage.setItem(pendingKey, JSON.stringify({ id, at: Date.now(), resumeLogin }));
  } catch {
    // The gateway return also supplies the order locator when storage is blocked.
  }
}
export function paymentLoginReturnPath(serverPath?: string) {
  try {
    const saved = JSON.parse(sessionStorage.getItem(pendingKey) ?? 'null');
    if (
      saved?.resumeLogin === true &&
      typeof saved.id === 'string' &&
      idPattern.test(saved.id) &&
      typeof saved.at === 'number' &&
      Date.now() - saved.at >= 0 &&
      Date.now() - saved.at < 86400000
    )
      return `/?payment=returned&order=${saved.id}`;
  } catch {}
  if (typeof serverPath === 'string' && serverPath.startsWith('/?')) {
    const params = new URLSearchParams(serverPath.slice(2));
    const id = params.get('order') ?? '';
    if (
      params.getAll('payment').length === 1 &&
      params.get('payment') === 'returned' &&
      params.getAll('order').length === 1 &&
      idPattern.test(id)
    )
      return `/?payment=returned&order=${id}`;
  }
  return '';
}
export function paymentReturnTarget() {
  const params = new URLSearchParams(location.search);
  if (params.get('payment') !== 'returned') return { requested: false, id: '' };
  const supplied = params.get('order');
  if (supplied !== null) return { requested: true, id: idPattern.test(supplied) ? supplied : '' };
  try {
    const saved = JSON.parse(sessionStorage.getItem(pendingKey) ?? 'null');
    if (
      saved &&
      typeof saved.id === 'string' &&
      idPattern.test(saved.id) &&
      typeof saved.at === 'number' &&
      Date.now() - saved.at >= 0 &&
      Date.now() - saved.at < 86400000
    )
      return { requested: true, id: saved.id as string };
  } catch {}
  return { requested: true, id: '' };
}
export function clearPaymentReturn(id: string) {
  const url = new URL(location.href);
  // Gateway parameters must never linger as an apparent proof of payment.
  for (const name of ['payment', 'order', 'trade_status']) url.searchParams.delete(name);
  history.replaceState(history.state, '', url.pathname + url.search + url.hash);
  try {
    const saved = JSON.parse(sessionStorage.getItem(pendingKey) ?? 'null');
    if (saved?.id === id) sessionStorage.removeItem(pendingKey);
  } catch {}
}
