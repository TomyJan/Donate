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

it.each(['https://example.com/other.png', undefined])(
  'retries an earlier avatar after the source changes through %s',
  (intermediate) => {
    const view = render(<Avatar name="A" src="https://example.com/avatar.png" />);
    fireEvent.error(view.container.querySelector('img') as HTMLImageElement);
    expect(view.container.querySelector('img')).toBeNull();

    view.rerender(<Avatar name="B" src={intermediate} />);
    view.rerender(<Avatar name="A" src="https://example.com/avatar.png" />);

    expect(view.container.querySelector('img')).toHaveAttribute('src', 'https://example.com/avatar.png');
  },
);
