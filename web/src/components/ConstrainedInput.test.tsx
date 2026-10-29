import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, expect, it } from 'vitest';
import { acceptAmountEdit } from '../lib/amount';
import { ConstrainedInput } from './ConstrainedInput';

function Amount() {
  const [value, setValue] = useState('20.26');
  return <ConstrainedInput aria-label="Amount" value={value} onValue={setValue} accept={acceptAmountEdit} />;
}

afterEach(cleanup);
it('rejects an invalid edit in the middle without moving the caret to the start', async () => {
  const user = userEvent.setup();
  render(<Amount />);
  const input = screen.getByRole('textbox') as HTMLInputElement;
  await user.click(input);
  await user.keyboard('{Home}{ArrowRight}x');
  expect(input).toHaveValue('20.26');
  expect(input.selectionStart).toBe(1);
  await user.keyboard('3');
  expect(input).toHaveValue('230.26');
});
