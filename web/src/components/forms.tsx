import * as Popover from '@radix-ui/react-popover';
import * as Select from '@radix-ui/react-select';
import { CalendarDays, Check, ChevronDown } from 'lucide-react';
import { useState } from 'react';
import { DayPicker } from 'react-day-picker';
import { useApp } from '../app/context';
import { Button, portal } from './ui';
export function SelectField({
  value,
  onValue,
  options,
  label,
  onCloseAutoFocus,
}: {
  value: string;
  onValue: (v: string) => void;
  options: [string, string][];
  label: string;
  onCloseAutoFocus?: (event: Event) => void;
}) {
  return (
    <Select.Root value={value || '__empty'} onValueChange={(v) => onValue(v === '__empty' ? '' : v)}>
      <Select.Trigger className="d-btn d-control a-btn a-select" aria-label={label}>
        <Select.Value />
        <Select.Icon>
          <ChevronDown />
        </Select.Icon>
      </Select.Trigger>
      <Select.Portal container={portal()}>
        <Select.Content
          className="d-pop select-pop"
          position="popper"
          sideOffset={7}
          collisionPadding={12}
          onCloseAutoFocus={onCloseAutoFocus}
        >
          <Select.Viewport>
            {options.map(([id, text]) => (
              <Select.Item key={id} value={id || '__empty'} className="d-option">
                <Select.ItemText>{text}</Select.ItemText>
                <Select.ItemIndicator className="d-tick">
                  <Check />
                </Select.ItemIndicator>
              </Select.Item>
            ))}
          </Select.Viewport>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  );
}
export function DateTimeField({
  value,
  onValue,
  label,
}: {
  value: string;
  onValue: (v: string) => void;
  label: string;
}) {
  const { t } = useApp();
  const date = value ? new Date(value) : undefined;
  const [open, setOpen] = useState(false);
  const update = (day: Date | undefined, hour?: string, minute?: string) => {
    if (!day) {
      onValue('');
      return;
    }
    const beijing = value ? new Date(new Date(value).getTime() + 8 * 3600000) : null;
    const h = hour ?? String(beijing?.getUTCHours() ?? 0),
      m = minute ?? String(beijing?.getUTCMinutes() ?? 0);
    const dateText = `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, '0')}-${String(day.getDate()).padStart(2, '0')}`;
    onValue(new Date(`${dateText}T${h.padStart(2, '0')}:${m.padStart(2, '0')}:00+08:00`).toISOString());
  };
  const b = date ? new Date(date.getTime() + 8 * 3600000) : undefined;
  const day = b ? new Date(b.getUTCFullYear(), b.getUTCMonth(), b.getUTCDate()) : undefined;
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <Button className="a-btn a-select" aria-label={label}>
          <span>
            {value
              ? `${day?.toLocaleDateString()} ${String(b?.getUTCHours()).padStart(2, '0')}:${String(b?.getUTCMinutes()).padStart(2, '0')}`
              : label}
          </span>
          <CalendarDays />
        </Button>
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content className="d-pop date-pop" sideOffset={8} collisionPadding={12}>
          <DayPicker mode="single" selected={day} onSelect={(v) => update(v)} showOutsideDays />
          <div className="d-row">
            <SelectField
              label="Hour"
              value={String(b?.getUTCHours() ?? 0)}
              onValue={(h) => update(day ?? new Date(), h)}
              options={Array.from({ length: 24 }, (_, i) => [String(i), String(i).padStart(2, '0')])}
            />
            <span>:</span>
            <SelectField
              label="Minute"
              value={String(b?.getUTCMinutes() ?? 0)}
              onValue={(m) => update(day ?? new Date(), undefined, m)}
              options={Array.from({ length: 60 }, (_, i) => [String(i), String(i).padStart(2, '0')])}
            />
          </div>
          <div className="d-row">
            <Button className="d-ghost" onClick={() => onValue('')}>
              {t('clear')}
            </Button>
            <Button onClick={() => setOpen(false)}>{t('confirm')}</Button>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
