import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import { OverviewRange } from './OverviewRange';

const { notify } = vi.hoisted(() => ({ notify: vi.fn() }));
vi.mock('../app/context', () => ({ useApp: () => ({ t: (key: string) => translate(key, 'en'), notify }) }));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
it('allows applying an invalid custom range and explains it with a toast instead of inline text', async () => {
  const onChange = vi.fn();
  render(
    <div className="d-app">
      <OverviewRange
        value={{ range: 'custom', from: '2026-10-30T00:00:00Z', to: '2026-10-29T00:00:00Z' }}
        onChange={onChange}
      />
    </div>,
  );
  fireEvent.click(screen.getByRole('button', { name: translate('customRange', 'en') }));
  const apply = await screen.findByRole('button', { name: 'Apply' });
  expect(apply).toBeEnabled();
  fireEvent.click(apply);
  expect(notify).toHaveBeenCalledWith(translate('dateRule', 'en'), true);
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.queryByRole('alert')).toBeNull();
});
