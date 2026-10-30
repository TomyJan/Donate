import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import { DateTimeField } from './forms';

vi.mock('../app/context', () => ({
  useApp: () => ({ lang: 'en', t: (key: string) => translate(key, 'en') }),
}));
const originalScroll = Element.prototype.scrollIntoView;
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => {
  cleanup();
  Element.prototype.scrollIntoView = originalScroll;
});
function Example({ initial, changed }: { initial: string; changed: (value: string) => void }) {
  const [value, setValue] = useState(initial);
  return (
    <div className="d-app">
      <DateTimeField
        label="From"
        value={value}
        onValue={(next) => {
          setValue(next);
          changed(next);
        }}
      />
      <output>{value}</output>
    </div>
  );
}
async function choose(label: string, option: string) {
  fireEvent.keyDown(screen.getByRole('combobox', { name: translate(label, 'en') }), { key: 'ArrowDown' });
  fireEvent.keyDown(await screen.findByRole('option', { name: option }), { key: 'Enter' });
  await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
}
function pick(day: string) {
  const button = document.querySelector<HTMLButtonElement>(`[data-day="${day}"] button`);
  if (!button) throw new Error(`Calendar day unavailable: ${day}`);
  fireEvent.click(button);
}

it('opens the selected Shanghai month instead of the current system month', () => {
  render(<Example initial="2024-01-31T15:47:00Z" changed={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'From' }));
  expect(screen.getByRole('grid')).toHaveAttribute('aria-label', 'January 2024');
});

it('browses year and month without applying a date and preserves 23:47 when selecting a day', async () => {
  const changed = vi.fn();
  render(<Example initial="2024-01-31T15:47:00Z" changed={changed} />);
  fireEvent.click(screen.getByRole('button', { name: 'From' }));
  await choose('calendarYear', '2025');
  await choose('calendarMonth', 'February');
  expect(screen.getByRole('grid')).toHaveAttribute('aria-label', 'February 2025');
  expect(changed).not.toHaveBeenCalled();
  expect(screen.getByText('2024-01-31T15:47:00Z', { selector: 'output' })).toBeInTheDocument();
  pick('2025-02-28');
  expect(changed).toHaveBeenLastCalledWith('2025-02-28T15:47:00.000Z');
});

it('changes leap-year views without normalizing February into March', async () => {
  const changed = vi.fn();
  render(<Example initial="2024-02-29T15:47:00Z" changed={changed} />);
  fireEvent.click(screen.getByRole('button', { name: 'From' }));
  await choose('calendarYear', '2023');
  expect(screen.getByRole('grid')).toHaveAttribute('aria-label', 'February 2023');
  expect(changed).not.toHaveBeenCalled();
  pick('2023-02-28');
  expect(changed).toHaveBeenLastCalledWith('2023-02-28T15:47:00.000Z');
});

it('uses the Shanghai day across the UTC year boundary and keeps minutes after month navigation', async () => {
  const changed = vi.fn();
  render(<Example initial="2026-12-31T17:15:00Z" changed={changed} />);
  fireEvent.click(screen.getByRole('button', { name: 'From' }));
  expect(screen.getByRole('grid')).toHaveAttribute('aria-label', 'January 2027');
  fireEvent.click(screen.getByRole('button', { name: translate('calendarPreviousMonth', 'en') }));
  expect(screen.getByRole('grid')).toHaveAttribute('aria-label', 'December 2026');
  expect(changed).not.toHaveBeenCalled();
  pick('2026-12-31');
  expect(changed).toHaveBeenLastCalledWith('2026-12-30T17:15:00.000Z');
});
