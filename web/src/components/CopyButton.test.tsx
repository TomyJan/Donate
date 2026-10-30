import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { CopyButton } from './CopyButton';

const { report, notify } = vi.hoisted(() => ({ report: vi.fn(), notify: vi.fn() }));
vi.mock('../app/context', () => ({
  useApp: () => ({ t: (key: string) => (key === 'copySuccess' ? 'Copied' : key), report, notify }),
}));

beforeEach(() => {
  vi.useFakeTimers();
  report.mockClear();
  notify.mockClear();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it('copies the supplied information and restores its label after one second without a toast', async () => {
  let clipboard = '';
  vi.stubGlobal('navigator', {
    clipboard: {
      writeText: async (value: string) => {
        clipboard = value;
      },
    },
  });
  render(<CopyButton text={'Order 123\nAmount: ¥9.88'} label="Copy info" />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy info' }));
  await act(async () => {});
  expect(clipboard).toBe('Order 123\nAmount: ¥9.88');
  expect(screen.getByRole('button', { name: 'Copied' })).toHaveAttribute('data-copied', 'true');
  expect(notify).not.toHaveBeenCalled();
  await act(async () => {
    vi.advanceTimersByTime(999);
  });
  expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument();
  await act(async () => {
    vi.advanceTimersByTime(1);
  });
  expect(screen.getByRole('button', { name: 'Copy info' })).toHaveAttribute('data-copied', 'false');
});

it('reports a clipboard failure and keeps the button ready to retry', async () => {
  const failure = new Error('Clipboard denied');
  vi.stubGlobal('navigator', {
    clipboard: {
      writeText: async () => {
        throw failure;
      },
    },
  });
  render(<CopyButton text="order-id" label="Copy ID" />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy ID' }));
  await act(async () => {});
  expect(report).toHaveBeenCalledWith(failure);
  expect(screen.getByRole('button', { name: 'Copy ID' })).toBeEnabled();
  expect(screen.queryByRole('button', { name: 'Copied' })).not.toBeInTheDocument();
});

it('does not apply an older copy result to different information', async () => {
  let finish: (() => void) | undefined;
  vi.stubGlobal('navigator', {
    clipboard: {
      writeText: () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    },
  });
  const view = render(<CopyButton text="first-order" label="Copy ID" />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy ID' }));
  view.rerender(<CopyButton text="second-order" label="Copy ID" />);
  await act(async () => {
    finish?.();
  });
  expect(screen.getByRole('button', { name: 'Copy ID' })).toBeEnabled();
  expect(screen.queryByRole('button', { name: 'Copied' })).not.toBeInTheDocument();
});

it('clears its success feedback timer when the button unmounts', async () => {
  vi.stubGlobal('navigator', { clipboard: { writeText: async () => {} } });
  const view = render(<CopyButton text="order-id" label="Copy ID" />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy ID' }));
  await act(async () => {});
  expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument();
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
});
