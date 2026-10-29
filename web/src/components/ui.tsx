import * as Dialog from '@radix-ui/react-dialog';
import * as Dropdown from '@radix-ui/react-dropdown-menu';
import { ArrowLeft, ArrowRight, Check, X } from 'lucide-react';
import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { useState } from 'react';
import { useApp } from '../app/context';
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
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  title: string;
  children: ReactNode;
  wide?: boolean;
}) {
  const { t } = useApp();
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={portal()}>
        <Dialog.Overlay className="d-overlay" />
        <Dialog.Content
          className={`d-dialog d-dialog-frame ${wide ? 'dialog-wide' : ''}`}
          aria-describedby={undefined}
        >
          <div className="d-dialog-head">
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Close asChild>
              <Button className="d-icon d-ghost" aria-label={t('close')}>
                <X />
              </Button>
            </Dialog.Close>
          </div>
          {children}
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
}: {
  page: number;
  total: number;
  size: number;
  onPage: (v: number) => void;
}) {
  const { t } = useApp();
  const count = Math.max(1, Math.ceil(total / size));
  return (
    <div className="a-foot">
      <span>
        {t('total')} {total} {t('items')}
      </span>
      <div className="a-pagination">
        <Button
          className="d-icon d-ghost"
          aria-label={t('previous')}
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
        >
          <ArrowLeft />
        </Button>
        <span>
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
