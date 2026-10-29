import type { InputHTMLAttributes } from 'react';
import { useRef } from 'react';

type Props = Omit<InputHTMLAttributes<HTMLInputElement>, 'onChange' | 'value' | 'accept'> & {
  value: string;
  onValue: (value: string) => void;
  accept: (value: string) => boolean;
};
export function ConstrainedInput({ value, onValue, accept, ...props }: Props) {
  const selection = useRef<[number, number]>([value.length, value.length]);
  return (
    <input
      {...props}
      value={value}
      onSelect={(e) => {
        selection.current = [e.currentTarget.selectionStart ?? 0, e.currentTarget.selectionEnd ?? 0];
      }}
      onBeforeInput={(e) => {
        const input = e.currentTarget;
        const data = (e.nativeEvent as InputEvent).data;
        if (data != null) {
          const start = input.selectionStart ?? 0,
            end = input.selectionEnd ?? 0;
          const next = value.slice(0, start) + data + value.slice(end);
          if (!accept(next)) e.preventDefault();
        }
      }}
      onPaste={(e) => {
        const input = e.currentTarget;
        const next =
          value.slice(0, input.selectionStart ?? 0) +
          e.clipboardData.getData('text') +
          value.slice(input.selectionEnd ?? 0);
        if (!accept(next)) e.preventDefault();
      }}
      onChange={(e) => {
        const input = e.currentTarget;
        if (accept(input.value)) {
          onValue(input.value);
        } else {
          input.value = value;
          input.setSelectionRange(...selection.current);
        }
      }}
    />
  );
}
