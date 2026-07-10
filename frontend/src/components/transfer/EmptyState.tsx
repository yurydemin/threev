import { Inbox } from 'lucide-react';

export interface EmptyStateProps {
  message: string;
}

/**
 * Empty state per docs/03-ux-ui-spec.md section 5.5, styled after
 * `ConnectionList`'s empty state (icon + `text-sm text-fg-secondary`
 * message), minus the CTA button — there's no single obvious action to
 * offer here (unlike "Добавить подключение", queuing a transfer happens
 * from the File Manager, Block J).
 */
export function EmptyState({ message }: EmptyStateProps) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 py-16 text-center">
      <Inbox className="h-12 w-12 text-fg-muted" aria-hidden="true" />
      <p className="text-sm text-fg-secondary">{message}</p>
    </div>
  );
}
