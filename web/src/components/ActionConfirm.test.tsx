import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { ActionConfirm } from './ActionConfirm';

const { report } = vi.hoisted(() => ({ report: vi.fn() }));
vi.mock('../app/context', () => ({ useApp: () => ({ t: (key: string) => key, report }) }));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  report.mockClear();
});

it('waits for the preview and only executes the prepared operation once', async () => {
  let finish: ((value: string) => void) | undefined;
  const executed: string[] = [];
  render(
    <ActionConfirm
      trigger={<button type="button">Delete order</button>}
      title="Delete order?"
      prepare={() =>
        new Promise<string>((resolve) => {
          finish = resolve;
        })
      }
      onConfirm={async (operation) => {
        executed.push(operation);
      }}
    >
      <p>Preview required</p>
    </ActionConfirm>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Delete order' }));
  expect(screen.getByRole('button', { name: 'confirm' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
  expect(executed).toEqual([]);
  await act(async () => {
    finish?.('prepared-operation');
  });
  const confirm = screen.getByRole('button', { name: 'confirm' });
  fireEvent.click(confirm);
  fireEvent.click(confirm);
  await act(async () => {});
  expect(executed).toEqual(['prepared-operation']);
  expect(screen.queryByText('Preview required')).not.toBeInTheDocument();
});

it('discards a preview when the order changes before it resolves', async () => {
  let finish: ((value: string) => void) | undefined;
  const executed: string[] = [];
  const props = {
    trigger: <button type="button">Delete</button>,
    title: 'Delete?',
    prepare: () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
    onConfirm: async (value: string) => {
      executed.push(value);
    },
  };
  const view = render(<ActionConfirm {...props} scopeKey="first-order" />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  view.rerender(<ActionConfirm {...props} scopeKey="second-order" />);
  await act(async () => {
    finish?.('first-operation');
  });
  expect(screen.queryByRole('button', { name: 'confirm' })).not.toBeInTheDocument();
  expect(executed).toEqual([]);
});

it('refreshes an expired preview and requires a new confirmation before execution', async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-10-30T00:00:00Z'));
  let count = 0;
  const executed: string[] = [];
  render(
    <ActionConfirm
      trigger={<button type="button">Export</button>}
      title="Export?"
      prepare={async () => ({ id: `operation-${++count}`, expires: Date.now() + 100 })}
      expiresAt={(prepared) => prepared.expires}
      onConfirm={async (prepared) => {
        executed.push(prepared.id);
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Export' }));
  await act(async () => {});
  await act(async () => {
    vi.advanceTimersByTime(101);
  });
  fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
  await act(async () => {});
  expect(executed).toEqual([]);
  expect(screen.getByRole('alert')).toHaveTextContent('confirmationExpired');
  fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
  await act(async () => {});
  expect(executed).toEqual(['operation-2']);
});

it('reports a failed preview without offering an executable confirmation', async () => {
  const failure = new Error('Preview unavailable');
  render(
    <ActionConfirm
      trigger={<button type="button">Hide</button>}
      title="Hide?"
      prepare={async () => {
        throw failure;
      }}
      onConfirm={async () => {
        throw new Error('must not execute');
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
  await act(async () => {});
  expect(screen.getByRole('button', { name: 'confirm' })).toBeDisabled();
  expect(screen.getByRole('alert')).toHaveTextContent('error');
  expect(report).toHaveBeenCalledWith(failure);
});

it('requires a fresh preview after cancel and ignores the older pending preview', async () => {
  const pending: ((operation: string) => void)[] = [];
  const executed: string[] = [];
  render(
    <ActionConfirm
      trigger={<button type="button">Delete</button>}
      title="Delete?"
      prepare={() =>
        new Promise<string>((resolve) => {
          pending.push(resolve);
        })
      }
      onConfirm={async (operation) => {
        executed.push(operation);
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  fireEvent.click(screen.getByRole('button', { name: 'cancel' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  await act(async () => {
    pending[1]?.('second-operation');
  });
  await act(async () => {
    pending[0]?.('first-operation');
  });
  fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
  await act(async () => {});
  expect(executed).toEqual(['second-operation']);
});

it('keeps an executing confirmation open and blocks duplicate execution and dismissal', async () => {
  let finish: (() => void) | undefined;
  let executions = 0;
  render(
    <ActionConfirm
      trigger={<button type="button">Close</button>}
      title="Close?"
      onConfirm={() => {
        executions += 1;
        return new Promise<void>((resolve) => {
          finish = resolve;
        });
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Close' }));
  await act(async () => {});
  fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
  expect(screen.getByRole('button', { name: 'cancel' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'loading' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'loading' }));
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  expect(executions).toBe(1);
  await act(async () => {
    finish?.();
  });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

it('reports an execution failure and leaves the confirmation available for a deliberate retry', async () => {
  const failure = new Error('Operation unavailable');
  render(
    <ActionConfirm
      trigger={<button type="button">Hide</button>}
      title="Hide?"
      onConfirm={async () => {
        throw failure;
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
  await act(async () => {});
  fireEvent.click(screen.getByRole('button', { name: 'confirm' }));
  await act(async () => {});
  expect(screen.getByRole('alert')).toHaveTextContent('error');
  expect(screen.getByRole('button', { name: 'confirm' })).toBeEnabled();
  expect(report).toHaveBeenCalledWith(failure);
});

it('returns keyboard focus to the original trigger when Escape cancels a confirmation', async () => {
  const user = userEvent.setup();
  render(
    <ActionConfirm trigger={<button type="button">Hide</button>} title="Hide?" onConfirm={async () => {}} />,
  );
  const trigger = screen.getByRole('button', { name: 'Hide' });
  await user.click(trigger);
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  await user.keyboard('{Escape}');
  await waitFor(() => expect(trigger).toHaveFocus());
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});
