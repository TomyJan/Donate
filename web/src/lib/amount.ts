export function acceptAmountEdit(v: string) {
  return /^\d{0,6}(?:\.\d{0,2})?$/.test(v) && (v === '' || v === '.' || Number(v) <= 100000);
}
export function amountFen(v: string): number | null {
  if (!acceptAmountEdit(v) || !v || v === '.') return null;
  const [a, b = ''] = v.split('.');
  const n = Number(a || '0') * 100 + Number(b.padEnd(2, '0'));
  return n >= 1 && n <= 10000000 ? n : null;
}
export function acceptQQEdit(v: string) {
  return /^(?:[1-9]\d{0,11})?$/.test(v);
}
