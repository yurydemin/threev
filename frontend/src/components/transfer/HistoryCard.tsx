import { CheckCircle2, XCircle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { getTransferDisplayName, getTransferPathLine, getTypeLabel, getTypeTagClasses } from './transferDisplay';
import type { TransferHistoryEntry } from '../../types';

export interface HistoryCardProps {
  entry: TransferHistoryEntry;
}

/**
 * Card for a single `history` entry (`completed`/`cancelled`), per
 * docs/03-ux-ui-spec.md section 5.5 "Завершённые": same top/middle rows as
 * `TransferCard`, no progress bar, no action buttons (a finished transfer
 * isn't managed anymore).
 *
 * `cancelled` isn't covered by the spec's success/failed pair — rendered
 * as a muted `XCircle` + "Отменено" per Block I task notes, since a
 * user-cancelled task's `errorMessage` is normally empty. `errorMessage` is
 * still rendered when present (covers the theoretical `failed` case, which
 * the backend doesn't currently archive to history, but the type allows).
 */
export function HistoryCard({ entry }: HistoryCardProps) {
  const { t } = useTranslation();
  const isCompleted = entry.status === 'completed';

  return (
    <div className="flex flex-col gap-2 rounded border border-border bg-bg-secondary p-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn('shrink-0 text-2xs font-semibold uppercase', getTypeTagClasses(entry))}>
            {getTypeLabel(entry)}
          </span>
          <span className="truncate text-sm font-medium text-fg-primary" title={getTransferDisplayName(entry)}>
            {getTransferDisplayName(entry)}
          </span>
        </div>

        <div className="flex shrink-0 items-center gap-1.5">
          {isCompleted ? (
            <CheckCircle2 className="h-4 w-4 text-success" aria-hidden="true" />
          ) : (
            <XCircle className="h-4 w-4 text-fg-secondary" aria-hidden="true" />
          )}
          <span className={cn('text-2xs', isCompleted ? 'text-success' : 'text-fg-secondary')}>
            {isCompleted ? t('transfers.historyCard.completed') : entry.status === 'cancelled' ? t('transfers.historyCard.cancelled') : entry.status}
          </span>
        </div>
      </div>

      <p className="truncate font-mono text-xs text-fg-secondary" title={getTransferPathLine(entry)}>
        {getTransferPathLine(entry)}
      </p>

      {entry.errorMessage && (
        <p className="truncate text-2xs text-danger" title={entry.errorMessage}>
          {entry.errorMessage}
        </p>
      )}
    </div>
  );
}
