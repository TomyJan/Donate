import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import { Avatar } from './ui';

afterEach(cleanup);
it('loads a new identity avatar after the previous image failed', () => {
  const view = render(<Avatar name="A" src="https://example.com/old.png" />);
  const old = view.container.querySelector('img');
  expect(old).not.toBeNull();
  fireEvent.error(old as HTMLImageElement);
  view.rerender(<Avatar name="B" src="https://example.com/new.png" />);
  expect(view.container.querySelector('img')).toHaveAttribute('src', 'https://example.com/new.png');
});
