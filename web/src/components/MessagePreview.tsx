import * as Popover from '@radix-ui/react-popover';
import { useApp } from '../app/context';
import { portal } from './ui';

export function MessagePreview({ message }: { message?: string }) {
  const { t } = useApp();
  if (!message) return <span className="d-secondary">—</span>;
  return (
    <Popover.Root>
      <Popover.Trigger className="text-button message-preview" aria-label={t('readMessage')}>
        {message}
      </Popover.Trigger>
      <Popover.Portal container={portal()}>
        <Popover.Content
          className="d-pop message-pop"
          sideOffset={8}
          collisionPadding={16}
          aria-label={t('messageColumn')}
        >
          <p>{message}</p>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
