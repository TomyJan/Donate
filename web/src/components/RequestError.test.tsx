import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { APIError } from '../lib/api';
import { translate } from '../lib/i18n';
import { RequestError } from './RequestError';

const login = vi.hoisted(() => vi.fn());
vi.mock('../app/context', () => ({
  useApp: () => ({ t: (key: string) => translate(key, 'zh-CN'), setLoginOpen: login }),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
it('offers a local retry for a connection error', () => {
  const retry = vi.fn();
  render(<RequestError error={new APIError('OFFLINE', '', 0)} onRetry={retry} />);
  expect(screen.getByRole('status')).toHaveTextContent('网络已断开');
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  expect(retry).toHaveBeenCalledOnce();
});
it('opens sign in instead of retrying an expired-session request', () => {
  const retry = vi.fn();
  render(<RequestError error={new APIError('UNAUTHORIZED', '', 401)} onRetry={retry} />);
  fireEvent.click(screen.getByRole('button', { name: '登录' }));
  expect(login).toHaveBeenCalledWith(true);
  expect(retry).not.toHaveBeenCalled();
});
it('does not offer repeated requests for a deleted record', () => {
  render(<RequestError error={new APIError('NOT_FOUND', '', 200)} onRetry={vi.fn()} />);
  expect(screen.getByRole('status')).toHaveTextContent('已被删除');
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
});
