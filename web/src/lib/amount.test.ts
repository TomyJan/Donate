import { describe, expect, it } from 'vitest';
import { acceptAmountEdit, acceptQQEdit, amountFen } from './amount';

describe('amount editing', () => {
  it('preserves valid intermediate edits but blocks decimals, exponents and over-limit values', () => {
    for (const v of ['', '.', '0.', '20.26', '100000.00']) expect(acceptAmountEdit(v)).toBe(true);
    for (const v of ['1e3', '-1', '1.234', '100000.01', 'abc']) expect(acceptAmountEdit(v)).toBe(false);
    expect(amountFen('0')).toBe(null);
    expect(amountFen('.01')).toBe(1);
    expect(amountFen('20.26')).toBe(2026);
    expect(amountFen('100000')).toBe(10000000);
  });
  it('rejects nonnumeric QQ edits', () => {
    expect(acceptQQEdit('12345678')).toBe(true);
    expect(acceptQQEdit('12a34')).toBe(false);
    expect(acceptQQEdit('01234')).toBe(false);
  });
});
