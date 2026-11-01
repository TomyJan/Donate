import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import type { Site } from '../lib/types';
import { IdentityBindings } from './IdentityBindings';

const state = vi.hoisted(() => ({ admin: false, report: vi.fn(), site: undefined as Site | undefined }));
vi.mock('../app/context', () => ({
  useApp: () => ({
    profile: { id: 'administrator', admin: state.admin },
    t: (key: string) => translate(key, 'en'),
    report: state.report,
    site: state.site,
  }),
}));
const bindings = [
  {
    id: 'qq-binding',
    provider: 'qq',
    nickname: 'QQ donor',
    avatar_url: '',
    bound_at: '2026-10-29T00:00:00Z',
  },
  {
    id: 'oidc-binding',
    provider: 'oidc',
    nickname: 'OIDC donor',
    avatar_url: '',
    bound_at: '2026-10-30T00:00:00Z',
  },
];
afterEach(() => {
  cleanup();
  state.admin = false;
  state.site = undefined;
  state.report.mockClear();
});

it('uses custom OIDC presentation while matching stored kinds and submitting the public binding ID', () => {
  state.site = {
    name: 'Donate',
    logo: '',
    csrf_token: '',
    payment_enabled: false,
    version: { version: 'test', commit: '', tag: '', built_at: '' },
    providers: ['campus', 'qq'],
    provider_details: [
      { id: 'campus', kind: 'oidc', name: 'Campus account', icon_url: 'https://cdn.test/icon.svg' },
    ],
  };
  const actions: string[] = [];
  const first = render(
    <IdentityBindings
      bindings={bindings.slice(0, 1)}
      available={state.site.providers}
      onBind={(id) => actions.push(id)}
    />,
  );
  expect(screen.getByText('Campus account')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Link' }));
  expect(actions).toEqual(['campus']);
  first.unmount();
  render(
    <IdentityBindings
      bindings={bindings}
      available={state.site.providers}
      onBind={(id) => actions.push(id)}
    />,
  );
  expect(screen.getByText('Campus account').closest('.d-bind')).toHaveTextContent('OIDC donor');
  expect(screen.queryByRole('button', { name: 'Link' })).not.toBeInTheDocument();
});

it('shows each linked time without offering an unlink action in a regular account dialog', () => {
  render(
    <IdentityBindings
      bindings={bindings}
      onUnbind={async () => {
        throw new Error('not authorized');
      }}
    />,
  );
  expect(screen.getByText('2026-10-29 08:00:00')).toHaveAttribute('dateTime', bindings[0].bound_at);
  expect(screen.getByText('2026-10-30 08:00:00')).toHaveAttribute('dateTime', bindings[1].bound_at);
  expect(screen.queryByRole('button', { name: /Unlink/ })).not.toBeInTheDocument();
});

it('requires an explicit administrator confirmation before unlinking the selected identity', async () => {
  state.admin = true;
  const removed: string[] = [];
  render(
    <IdentityBindings
      ownerID="target-user"
      bindings={bindings}
      onUnbind={async (id) => {
        removed.push(id);
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Unlink QQ' }));
  await act(async () => {});
  expect(removed).toEqual([]);
  const popup = screen.getByRole('dialog', { name: 'Unlink QQ' });
  fireEvent.click(within(popup).getByRole('button', { name: 'Confirm' }));
  await act(async () => {});
  expect(removed).toEqual(['qq-binding']);
});

it('keeps the last sign-in method visible but disables unlinking with an accessible explanation', () => {
  state.admin = true;
  render(
    <IdentityBindings ownerID="target-user" bindings={bindings.slice(0, 1)} onUnbind={async () => {}} />,
  );
  const unlink = screen.getByRole('button', { name: 'Unlink QQ' });
  expect(unlink).toBeDisabled();
  expect(unlink).toHaveAccessibleDescription('Keep at least one sign-in method.');
});

it('blocks parallel identity actions while one unlink request is executing', async () => {
  state.admin = true;
  let finish: (() => void) | undefined;
  render(
    <IdentityBindings
      ownerID="target-user"
      bindings={bindings}
      onUnbind={() =>
        new Promise<void>((resolve) => {
          finish = resolve;
        })
      }
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Unlink QQ' }));
  await act(async () => {});
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  expect(screen.getByRole('button', { name: 'Unlink OIDC' })).toBeDisabled();
  await act(async () => {
    finish?.();
  });
  expect(screen.getByRole('button', { name: 'Unlink OIDC' })).toBeEnabled();
});

it.each(['owner', 'bindings'])('revokes an open unlink confirmation when its %s changes', async (change) => {
  state.admin = true;
  const removed: string[] = [];
  const onUnbind = async (id: string) => {
    removed.push(id);
  };
  const view = render(<IdentityBindings ownerID="first-user" bindings={bindings} onUnbind={onUnbind} />);
  fireEvent.click(screen.getByRole('button', { name: 'Unlink QQ' }));
  await act(async () => {});
  expect(screen.getByRole('dialog', { name: 'Unlink QQ' })).toBeInTheDocument();
  view.rerender(
    <IdentityBindings
      ownerID={change === 'owner' ? 'second-user' : 'first-user'}
      bindings={change === 'bindings' ? bindings.slice(0, 1) : bindings}
      onUnbind={onUnbind}
    />,
  );
  expect(screen.queryByRole('dialog', { name: 'Unlink QQ' })).not.toBeInTheDocument();
  expect(removed).toEqual([]);
  if (change === 'bindings') expect(screen.getByRole('button', { name: 'Unlink QQ' })).toBeDisabled();
});
