import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import type { Audit, User } from '../lib/types';
import { AdminTable } from './AdminTable';

vi.mock('../app/context', () => ({ useApp: () => ({ t: (key: string) => translate(key, 'en') }) }));
afterEach(cleanup);

it.each([true, false])('labels a system task audit without an order or user action (exists=%s)', (exists) => {
  const audit = {
    id: 'audit-task',
    created_at: '2026-10-30T00:00:00Z',
    action: 'task.retry',
    actor: 'Admin',
    actor_id: 'admin',
    result: 'queued',
    target: exists ? 'xdb4' : '',
    target_exists: exists,
    target_id: 'update-task',
    target_type: 'task',
    ...(exists ? { target_resource: 'xdb4' } : {}),
  };
  render(<AdminTable {...options} kind="audit" rows={[audit]} onSort={() => {}} />);
  const target = document.querySelector('td[data-field=target]');
  expect(target).toHaveTextContent(exists ? 'Task · IPv4 XDB' : 'Task no longer exists');
  expect(target?.querySelector('button')).toBeNull();
  expect(target).not.toHaveTextContent('User no longer exists');
});
const options = {
  sort: { key: 'created_at', direction: 'desc' as const },
  selection: new Set<string>(),
  onToggle: () => {},
  onToggleAll: () => {},
  onDetails: () => {},
  onTarget: () => {},
};
it('labels the actual user creation timestamp as registration and paid totals as donation amount without changing sort keys', () => {
  const user: User = {
    id: 'user',
    nickname: 'Donor',
    avatar_url: '',
    status: 'active',
    admin: false,
    bindings: [],
    created_at: '2026-10-29T00:00:00Z',
    last_login_at: '2026-10-30T00:00:00Z',
    order_count: 1,
    paid_fen: 988,
  };
  const sorts: unknown[] = [];
  render(<AdminTable {...options} kind="users" rows={[user]} onSort={(sort) => sorts.push(sort)} />);
  fireEvent.click(screen.getByRole('button', { name: 'Registered at' }));
  expect(sorts).toEqual([{ key: 'created_at', direction: 'asc' }]);
  expect(screen.getByRole('columnheader', { name: 'Donation amount' })).toBeInTheDocument();
  expect(screen.queryByRole('columnheader', { name: 'Received' })).not.toBeInTheDocument();
  expect(document.querySelector('td[data-field=created_at]')).toHaveTextContent('2026-10-29 08:00');
  expect(document.querySelector('td[data-field=paid_fen]')).toHaveAttribute('data-label', 'Donation amount');
  expect(document.querySelector('td[data-field=paid_fen]')).toHaveTextContent('¥9.88');
});

it('distinguishes a successful unlink audit result from an account that was never linked', () => {
  const audit: Audit = {
    id: 'audit',
    created_at: '2026-10-30T00:00:00Z',
    action: 'user.identity_unbind',
    actor: 'Admin',
    actor_id: 'admin',
    result: 'unbound',
    target: 'Donor',
    target_exists: true,
    target_id: 'user',
    target_type: 'user',
    target_nickname: 'Donor',
  };
  render(<AdminTable {...options} kind="audit" rows={[audit]} onSort={() => {}} />);
  expect(document.querySelector('td[data-field=result]')).toHaveTextContent('Unlinked');
  expect(document.querySelector('td[data-field=result]')).not.toHaveTextContent('Not linked');
});
