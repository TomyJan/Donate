import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { translate } from '../lib/i18n';
import { LocationCheck } from './LocationCheck';

vi.mock('../app/context', () => ({
  useApp: () => ({ t: (key: string) => translate(key, 'en') }),
}));
afterEach(cleanup);

it('shows the latest verification independently of the preserved location', () => {
  render(
    <LocationCheck
      check={{
        country: '中国',
        province: '',
        city: '',
        district: '',
        street: '',
        isp: '',
        asn: '',
        source: 'unknown',
        version: '',
        queried_at: '2026-11-01T00:00:00Z',
        verification: 'conflict',
        conflict: true,
        evidence: [
          {
            source: 'xdb',
            country: '中国',
            province: '广东',
            city: '深圳',
            outcome: 'complete',
            queried_at: '2026-11-01T00:00:00Z',
          },
          {
            source: 'ip9',
            country: '中国',
            province: '浙江',
            outcome: 'complete',
            queried_at: '2026-11-01T00:00:00Z',
          },
        ],
      }}
    />,
  );
  const toggle = screen.getByRole('button', { name: /Location verification/ });
  expect(toggle).toHaveAttribute('aria-expanded', 'false');
  expect(screen.getByText('Unresolved conflict')).toBeInTheDocument();
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByText('中国 · 广东 · 深圳')).toBeInTheDocument();
  expect(screen.getByText('中国 · 浙江')).toBeInTheDocument();
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'false');
});

it('renders an unavailable source without pretending that it agrees', () => {
  render(
    <LocationCheck
      check={{
        country: '中国',
        province: '广东',
        city: '深圳',
        district: '',
        street: '',
        isp: '',
        asn: '',
        source: 'xdb',
        version: 'v3',
        queried_at: '2026-11-01T00:00:00Z',
        verification: 'single',
        evidence: [{ source: 'ip9', outcome: 'unavailable', queried_at: '2026-11-01T00:00:00Z' }],
      }}
    />,
  );
  expect(screen.getByText('Single source')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /Location verification/ }));
  expect(screen.getByText('Unavailable')).toBeInTheDocument();
});
