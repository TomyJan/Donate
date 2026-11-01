import { expect, it } from 'vitest';
import { dateTime } from './api';

it.each([
  ['2026-11-01T02:03:04Z', '2026-11-01 10:03:04'],
  ['2026-10-31T16:00:00Z', '2026-11-01 00:00:00'],
  ['2026-11-01T10:03:04+08:00', '2026-11-01 10:03:04'],
])('shows Shanghai timestamps to the second for %s', (value, expected) => {
  expect(dateTime(value)).toBe(expected);
});

it('keeps the donation list and receipt presentation at minute precision', () => {
  expect(dateTime('2026-11-01T02:03:49Z', 'minute')).toBe('2026-11-01 10:03');
});

it.each([undefined, null, ''])('keeps missing timestamps empty for %s', (value) => {
  expect(dateTime(value)).toBe('—');
});
