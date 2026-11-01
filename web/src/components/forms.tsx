import * as Popover from '@radix-ui/react-popover';
import * as Select from '@radix-ui/react-select';
import { CalendarDays, Check, ChevronDown, ChevronLeft, ChevronRight } from 'lucide-react';
import { useState } from 'react';
import { DayPicker, type MonthCaptionProps, TZDate, useDayPicker } from 'react-day-picker';
import { enUS, zhCN } from 'react-day-picker/locale';
import { useApp } from '../app/context';
import { Button, portal } from './ui';
import './DateTimeField.css';
export function SelectField({
  value,
  onValue,
  options,
  label,
  onCloseAutoFocus,
  className = '',
}: {
  value: string;
  onValue: (v: string) => void;
  options: [string, string][];
  label: string;
  onCloseAutoFocus?: (event: Event) => void;
  className?: string;
}) {
  return (
    <Select.Root value={value || '__empty'} onValueChange={(v) => onValue(v === '__empty' ? '' : v)}>
      <Select.Trigger className={`d-btn d-control a-btn a-select ${className}`} aria-label={label}>
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
const calendarZone = 'Asia/Shanghai';
function CalendarCaption({ calendarMonth, className, style }: MonthCaptionProps) {
  const { lang, t } = useApp();
  const { previousMonth, nextMonth, goToMonth } = useDayPicker();
  const month = calendarMonth.date;
  const year = month.getFullYear();
  const monthNames = Array.from({ length: 12 }, (_, index): [string, string] => [
    String(index),
    new Intl.DateTimeFormat(lang, { month: 'long', timeZone: calendarZone }).format(
      new TZDate(2024, index, 1, calendarZone),
    ),
  ]);
  return (
    <div className={className} style={style}>
      <div className="date-calendar-heading">
        <Button
          className="date-calendar-arrow"
          aria-label={t('calendarPreviousMonth')}
          disabled={!previousMonth}
          onClick={() => previousMonth && goToMonth(previousMonth)}
        >
          <ChevronLeft />
        </Button>
        <SelectField
          className="date-calendar-year"
          label={t('calendarYear')}
          value={String(year)}
          onValue={(next) => goToMonth(new TZDate(Number(next), month.getMonth(), 1, calendarZone))}
          options={Array.from({ length: 201 }, (_, index) => {
            const next = String(year - 100 + index);
            return [next, next];
          })}
        />
        <SelectField
          className="date-calendar-month"
          label={t('calendarMonth')}
          value={String(month.getMonth())}
          onValue={(next) => goToMonth(new TZDate(year, Number(next), 1, calendarZone))}
          options={monthNames}
        />
        <Button
          className="date-calendar-arrow"
          aria-label={t('calendarNextMonth')}
          disabled={!nextMonth}
          onClick={() => nextMonth && goToMonth(nextMonth)}
        >
          <ChevronRight />
        </Button>
      </div>
      <span className="date-calendar-announcement" role="status" aria-live="polite">
        {new Intl.DateTimeFormat(lang, { month: 'long', year: 'numeric', timeZone: calendarZone }).format(
          month,
        )}
      </span>
    </div>
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
  const { lang, t } = useApp();
  const date = value ? new TZDate(value, calendarZone) : undefined;
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState<Date>(() => date ?? TZDate.tz(calendarZone));
  const update = (day: Date | undefined, hour?: string, minute?: string, second?: string) => {
    if (!day) {
      onValue('');
      return;
    }
    const next = new TZDate(
      day.getFullYear(),
      day.getMonth(),
      day.getDate(),
      Number(hour ?? date?.getHours() ?? 0),
      Number(minute ?? date?.getMinutes() ?? 0),
      Number(second ?? date?.getSeconds() ?? 0),
      calendarZone,
    );
    onValue(new Date(next.getTime()).toISOString());
  };
  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        if (next) setMonth(date ?? TZDate.tz(calendarZone));
        setOpen(next);
      }}
    >
      <Popover.Trigger asChild>
        <Button className="a-btn a-select" aria-label={label}>
          <span>
            {value
              ? `${date?.toLocaleDateString(lang, { timeZone: calendarZone })} ${String(date?.getHours()).padStart(2, '0')}:${String(date?.getMinutes()).padStart(2, '0')}:${String(date?.getSeconds()).padStart(2, '0')}`
              : label}
          </span>
          <CalendarDays />
        </Button>
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content className="d-pop date-pop date-time-pop" sideOffset={8} collisionPadding={12}>
          <DayPicker
            mode="single"
            selected={date}
            month={month}
            onMonthChange={setMonth}
            onSelect={(v) => update(v)}
            showOutsideDays
            hideNavigation
            timeZone={calendarZone}
            locale={lang === 'en' ? enUS : zhCN}
            components={{ MonthCaption: CalendarCaption }}
          />
          <div className="d-row date-time-controls">
            <SelectField
              label="Hour"
              value={String(date?.getHours() ?? 0)}
              onValue={(h) => update(date ?? TZDate.tz(calendarZone), h)}
              options={Array.from({ length: 24 }, (_, i) => [String(i), String(i).padStart(2, '0')])}
            />
            <span>:</span>
            <SelectField
              label="Minute"
              value={String(date?.getMinutes() ?? 0)}
              onValue={(m) => update(date ?? TZDate.tz(calendarZone), undefined, m)}
              options={Array.from({ length: 60 }, (_, i) => [String(i), String(i).padStart(2, '0')])}
            />
            <span>:</span>
            <SelectField
              label={t('calendarSecond')}
              value={String(date?.getSeconds() ?? 0)}
              onValue={(s) => update(date ?? TZDate.tz(calendarZone), undefined, undefined, s)}
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
