import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';
import { BackToTop, ScrollHeader } from './ScrollChrome';

vi.mock('../app/context', () => ({ useApp: () => ({ t: (key: string) => key }) }));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
async function scrollTo(position: number) {
  vi.stubGlobal('scrollY', position);
  fireEvent.scroll(window);
  await act(async () => {
    await new Promise(requestAnimationFrame);
  });
}
it('tracks partial scroll distance without finishing a hide or reveal after the gesture stops', async () => {
  vi.stubGlobal('scrollY', 0);
  const { container } = render(
    <MemoryRouter>
      <ScrollHeader>Header</ScrollHeader>
    </MemoryRouter>,
  );
  const header = container.querySelector('header') as HTMLElement;
  Object.defineProperty(header, 'offsetHeight', { configurable: true, value: 104 });
  await scrollTo(50);
  await waitFor(() => expect(header.style.transform).toContain('-50px'));
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 300));
  });
  expect(header.style.transform).toContain('-50px');
  await scrollTo(250);
  await waitFor(() => expect(header).toHaveAttribute('data-hidden', 'true'));
  await scrollTo(230);
  await waitFor(() => expect(header.style.transform).toContain('-84px'));
  await scrollTo(0);
  await waitFor(() => expect(header).toHaveAttribute('data-hidden', 'false'));
});
it('offers back to top only after scrolling down and then up away from the top', async () => {
  vi.stubGlobal('scrollY', 0);
  render(<BackToTop />);
  await scrollTo(650);
  expect(screen.queryByRole('button', { name: 'backToTop' })).not.toBeInTheDocument();
  await scrollTo(620);
  expect(await screen.findByRole('button', { name: 'backToTop' })).toBeInTheDocument();
  await scrollTo(680);
  await waitFor(() => expect(screen.queryByRole('button', { name: 'backToTop' })).not.toBeInTheDocument());
  await scrollTo(100);
  expect(screen.queryByRole('button', { name: 'backToTop' })).not.toBeInTheDocument();
});
