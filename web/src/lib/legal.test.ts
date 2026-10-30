import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LEGAL_CONSENT_KEY, LEGAL_VERSION, useLocalAcknowledgment } from './legal';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.clear();
});

it('requires a fresh opt-in for an old or malformed version and persists revocation', () => {
  localStorage.setItem(LEGAL_CONSENT_KEY, 'old-version');
  const first = renderHook(() => useLocalAcknowledgment(LEGAL_CONSENT_KEY));
  expect(first.result.current[0]).toBe(false);
  act(() => first.result.current[1](true));
  first.unmount();
  const second = renderHook(() => useLocalAcknowledgment(LEGAL_CONSENT_KEY));
  expect(second.result.current[0]).toBe(true);
  act(() => second.result.current[1](false));
  second.unmount();
  const third = renderHook(() => useLocalAcknowledgment(LEGAL_CONSENT_KEY));
  expect(third.result.current[0]).toBe(false);
});

it('keeps the current tab usable when browser storage is disabled', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new DOMException('Blocked', 'SecurityError');
  });
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new DOMException('Blocked', 'QuotaExceededError');
  });
  const { result } = renderHook(() => useLocalAcknowledgment(LEGAL_CONSENT_KEY));
  expect(result.current[0]).toBe(false);
  act(() => result.current[1](true));
  expect(result.current[0]).toBe(true);
});

it('reflects another tab accepting and clearing its storage without a reload', () => {
  const { result } = renderHook(() => useLocalAcknowledgment(LEGAL_CONSENT_KEY));
  act(() => {
    localStorage.setItem(LEGAL_CONSENT_KEY, LEGAL_VERSION);
    window.dispatchEvent(new StorageEvent('storage', { key: LEGAL_CONSENT_KEY, storageArea: localStorage }));
  });
  expect(result.current[0]).toBe(true);
  act(() => {
    localStorage.clear();
    window.dispatchEvent(new StorageEvent('storage', { key: null, storageArea: localStorage }));
  });
  expect(result.current[0]).toBe(false);
});
