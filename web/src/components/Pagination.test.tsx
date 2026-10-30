import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { Pagination } from './ui';

vi.mock('../app/context', () => ({ useApp: () => ({ t: (key: string) => key }) }));
const originalScroll = Element.prototype.scrollIntoView;
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => {
  cleanup();
  Element.prototype.scrollIntoView = originalScroll;
});
function Example({ total }: { total: number }) {
  const [size, setSize] = useState(20);
  const [page, setPage] = useState(1);
  return <Pagination total={total} size={size} page={page} onSize={setSize} onPage={setPage} />;
}
it('offers only the smallest page size when all seven rows fit in it', async () => {
  render(<Example total={7} />);
  const trigger = screen.getByRole('combobox', { name: 'perPageCount' });
  await waitFor(() => expect(trigger).toHaveTextContent('10'));
  fireEvent.keyDown(trigger, { key: 'ArrowDown' });
  expect(await screen.findAllByRole('option')).toHaveLength(1);
});
it('keeps the next useful bucket without offering sizes larger than it', async () => {
  render(<Example total={21} />);
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'perPageCount' }), { key: 'ArrowDown' });
  const options = await screen.findAllByRole('option');
  expect(options.map((option) => option.textContent?.trim())).toEqual([
    '10 perPage',
    '20 perPage',
    '50 perPage',
  ]);
});
