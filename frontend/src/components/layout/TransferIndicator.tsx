import { ArrowLeftRight } from 'lucide-react';

export interface TransferIndicatorProps {
  /** Number of tasks currently in `useTransferStore`'s `queue`. */
  count: number;
  /** Navigates to the Transfers screen. */
  onClick: () => void;
}

/**
 * Status-bar transfer indicator (Stage 3 Block K), rendered into
 * `StatusBar`'s `right` slot from `FileManagerScreen`.
 *
 * Renders nothing when `count === 0` — an empty queue has nothing to
 * announce, and a permanently-visible "0 активных передач" would just be
 * status-bar noise.
 */
export function TransferIndicator({ count, onClick }: TransferIndicatorProps) {
  if (count === 0) return null;

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-1.5 text-xs text-fg-secondary transition-colors duration-fast hover:text-accent"
    >
      <ArrowLeftRight className="h-3 w-3 shrink-0" aria-hidden="true" />
      {count} активных передач
    </button>
  );
}
