import { beforeEach, expect, it, vi } from 'vitest';
import {
  clearPaymentReturn,
  paymentLoginReturnPath,
  paymentReturnTarget,
  rememberPayment,
} from './payment-return';

const id = '68f15e68-143e-4b30-a2a9-87c0df4c5479';
beforeEach(() => {
  history.replaceState(null, '', '/');
  sessionStorage.clear();
  vi.useRealTimers();
});
it('only resumes an explicitly started payment sign-in and clears it after completion', () => {
  rememberPayment(id);
  expect(paymentLoginReturnPath()).toBe('');
  rememberPayment(id, true);
  expect(paymentLoginReturnPath()).toBe(`/?payment=returned&order=${id}`);
  clearPaymentReturn(id);
  expect(paymentLoginReturnPath()).toBe('');
});
it('accepts a valid server flow destination but never an external or ambiguous return', () => {
  expect(paymentLoginReturnPath(`/?payment=returned&order=${id}`)).toBe(`/?payment=returned&order=${id}`);
  for (const path of [
    `https://evil.test/?payment=returned&order=${id}`,
    `//evil.test/?payment=returned&order=${id}`,
    '/?payment=returned&order=bad',
    `/?payment=returned&order=${id}&order=other`,
  ])
    expect(paymentLoginReturnPath(path)).toBe('');
});
it('does not start a return from a pending checkout on an ordinary visit', () => {
  rememberPayment(id);
  expect(paymentReturnTarget()).toEqual({ requested: false, id: '' });
});
it('locates an order from the return without trusting success flags', () => {
  history.replaceState(null, '', `/?payment=returned&order=${id}&trade_status=TRADE_SUCCESS`);
  expect(paymentReturnTarget()).toEqual({ requested: true, id });
});
it('uses the saved checkout only when the gateway omits a locator', () => {
  rememberPayment(id);
  history.replaceState(null, '', '/?payment=returned');
  expect(paymentReturnTarget()).toEqual({ requested: true, id });
  history.replaceState(null, '', '/?payment=returned&order=bad');
  expect(paymentReturnTarget()).toEqual({ requested: true, id: '' });
});
it('rejects old saved checkouts and tolerates blocked browser storage', () => {
  vi.useFakeTimers();
  rememberPayment(id);
  vi.advanceTimersByTime(86400001);
  history.replaceState(null, '', '/?payment=returned');
  expect(paymentReturnTarget()).toEqual({ requested: true, id: '' });
  const read = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new DOMException('Blocked');
  });
  expect(paymentReturnTarget()).toEqual({ requested: true, id: '' });
  read.mockRestore();
  vi.useRealTimers();
});
it('clears return parameters without discarding another pending payment', () => {
  rememberPayment(id);
  history.replaceState(null, '', '/?payment=returned&order=other&trade_status=TRADE_SUCCESS&lang=en');
  clearPaymentReturn('other');
  expect(location.search).toBe('?lang=en');
  history.replaceState(null, '', '/?payment=returned');
  expect(paymentReturnTarget()).toEqual({ requested: true, id });
  clearPaymentReturn(id);
  history.replaceState(null, '', '/?payment=returned');
  expect(paymentReturnTarget()).toEqual({ requested: true, id: '' });
});
