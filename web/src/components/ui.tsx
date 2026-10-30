import * as Dialog from '@radix-ui/react-dialog';
import * as Dropdown from '@radix-ui/react-dropdown-menu';
import * as Select from '@radix-ui/react-select';
import { ArrowLeft, ArrowRight, Check, ChevronDown, X } from 'lucide-react';
import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { useState } from 'react';
import { useApp } from '../app/context';
import { AnimatedHeight } from './AnimatedHeight';
import './shared-controls.css';
export const portal = () => document.querySelector<HTMLElement>('.d-app');
export function Button({ className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button type="button" className={`d-btn d-control ${className}`} {...props} />;
}
export function Avatar({ name = '', src }: { name?: string; src?: string }) {
  const [failed, setFailed] = useState<string>();
  return (
    <span className="d-avatar">
      {src && failed !== src ? (
        <img src={src} alt="" referrerPolicy="no-referrer" onError={() => setFailed(src)} />
      ) : (
        name.slice(0, 1) || '·'
      )}
    </span>
  );
}
export function Modal({
  open,
  onOpenChange,
  title,
  children,
  wide = false,
  dismissible = true,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  title: string;
  children: ReactNode;
  wide?: boolean;
  dismissible?: boolean;
}) {
  const { t } = useApp();
  return (
    <Dialog.Root open={open} onOpenChange={(value) => (value || dismissible) && onOpenChange(value)}>
      <Dialog.Portal container={portal()}>
        <Dialog.Overlay className="d-overlay" />
        <Dialog.Content
          className={`d-dialog d-dialog-frame ${wide ? 'dialog-wide' : ''}`}
          aria-describedby={undefined}
          onEscapeKeyDown={(event) => {
            if (!dismissible) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (!dismissible) event.preventDefault();
          }}
        >
          <AnimatedHeight>
            <div className="d-dialog-head">
              <Dialog.Title>{title}</Dialog.Title>
              <Dialog.Close asChild>
                <Button className="d-icon d-ghost" aria-label={t('close')} disabled={!dismissible}>
                  <X />
                </Button>
              </Dialog.Close>
            </div>
            {children}
          </AnimatedHeight>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
export type MenuItem = {
  label: string;
  icon?: ReactNode;
  selected?: boolean;
  action: () => void;
  danger?: boolean;
  separator?: boolean;
};
export function Menu({ trigger, label, items }: { trigger: ReactNode; label: string; items: MenuItem[] }) {
  return (
    <Dropdown.Root>
      <Dropdown.Trigger asChild>{trigger}</Dropdown.Trigger>
      <Dropdown.Portal container={portal()}>
        <Dropdown.Content
          className="d-pop"
          sideOffset={8}
          collisionPadding={12}
          aria-label={label}
          align="end"
        >
          {items.map((item) =>
            item.separator ? (
              <Dropdown.Separator key={item.label} />
            ) : (
              <Dropdown.Item
                key={item.label}
                aria-label={item.label}
                className={`d-option ${item.danger ? 'is-danger' : ''}`}
                data-selected={item.selected || undefined}
                onSelect={item.action}
              >
                {item.icon}
                <span>{item.label}</span>
                {item.selected && <Check className="d-tick" />}
              </Dropdown.Item>
            ),
          )}
        </Dropdown.Content>
      </Dropdown.Portal>
    </Dropdown.Root>
  );
}
export function Empty({ text }: { text?: string }) {
  const { t } = useApp();
  return (
    <div className="a-empty r-empty" role="status">
      {text ?? t('empty')}
    </div>
  );
}
export function Pagination({
  page,
  total,
  size,
  onPage,
  onSize,
}: {
  page: number;
  total: number;
  size: number;
  onPage: (value: number) => void;
  onSize?: (value: number) => void;
}) {
  const { t } = useApp();
  const count = Math.max(1, Math.ceil(total / size));
  return (
    <div className="a-foot">
      <span className="pagination-total">
        {t('total')} {total} {t('items')}
      </span>
      <Select.Root
        value={String(size)}
        disabled={!onSize}
        onValueChange={(value) => {
          onSize?.(Number(value));
          onPage(1);
        }}
      >
        <Select.Trigger className="d-btn d-control pagination-size" aria-label={t('perPageCount')}>
          <Select.Value />
          <Select.Icon>
            <ChevronDown />
          </Select.Icon>
        </Select.Trigger>
        <Select.Portal container={portal()}>
          <Select.Content className="d-pop select-pop" position="popper" sideOffset={7} collisionPadding={12}>
            <Select.Viewport>
              {[10, 20, 50, 100].map((value) => (
                <Select.Item key={value} value={String(value)} className="d-option">
                  <Select.ItemText>
                    {value} {t('perPage')}
                  </Select.ItemText>
                  <Select.ItemIndicator className="d-tick">
                    <Check />
                  </Select.ItemIndicator>
                </Select.Item>
              ))}
            </Select.Viewport>
          </Select.Content>
        </Select.Portal>
      </Select.Root>
      <div className="a-pagination">
        <Button
          className="d-icon d-ghost"
          aria-label={t('previous')}
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
        >
          <ArrowLeft />
        </Button>
        <span aria-live="polite">
          {page} / {count}
        </span>
        <Button
          className="d-icon d-ghost"
          aria-label={t('next')}
          disabled={page >= count}
          onClick={() => onPage(page + 1)}
        >
          <ArrowRight />
        </Button>
      </div>
    </div>
  );
}
