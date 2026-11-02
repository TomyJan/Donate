import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import { Avatar } from './ui';

afterEach(cleanup);
it.each(['Alice', '', ' '])(
  'shows a neutral silhouette instead of initials for %j without an image',
  (name) => {
    const view = render(<Avatar name={name} />);
    expect(view.container.querySelector('.d-avatar svg')).toBeInTheDocument();
    expect(view.container.querySelector('.d-avatar')).toHaveTextContent('');
    expect(view.container.querySelector('img')).toBeNull();
  },
);

it('falls back to the same silhouette when an avatar image fails', () => {
  const view = render(<Avatar name="Alice" src="https://example.com/unavailable.png" />);
  fireEvent.error(view.container.querySelector('img') as HTMLImageElement);
  expect(view.container.querySelector('.d-avatar svg')).toBeInTheDocument();
  expect(view.container.querySelector('.d-avatar')).toHaveTextContent('');
});

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
