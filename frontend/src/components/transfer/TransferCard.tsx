import { Pause, Play, RotateCcw, X } from 'lucide-react';
import { cn, formatETA, formatSpeed } from '../../lib/utils';
import { Button } from '../ui/Button';
import { Tooltip } from '../ui/Tooltip';
import { ProgressBar } from '../ui/ProgressBar';
import { useTransferStore } from '../../stores/useTransferStore';
import { getProgressPercent, getTransferDisplayName, getTransferPathLine, getTypeLabel, getTypeTagClasses } from './transferDisplay';
import type { TransferTask } from '../../types';

export interface TransferCardProps {
  task: TransferTask;
  onPause: (id: number) => void;
  onResume: (id: number) => void;
  onCancel: (id: number) => void;
  onRetry: (id: number) => void;
}

/**
 * Card for a single `queue` task, per docs/03-ux-ui-spec.md section 5.5.
 * Renders one of three variants by `task.status`:
 *  - `pending`/`running`: [Pause][Cancel] + progress bar + percent/speed/ETA.
 *  - `paused`: [Resume][Cancel] + progress bar + percent (speed/ETA read as
 *    "—" — a paused task isn't moving).
 *  - `failed`: [Retry][Cancel], no progress bar, `task.errorMessage` instead
 *    of the percent/speed/ETA row (see Block I task notes: a failed task
 *    stays in `queue`, it's not archived to `history`, so it needs its own
 *    look here rather than borrowing `HistoryCard`'s).
 *
 * Speed/ETA come from `useTransferStore`'s `speedByTaskId`/`etaByTaskId`
 * maps (populated from "transfer:progress" events) rather than from `task`
 * itself, which carries neither field (see `useTransferStore.ts`).
 */
export function TransferCard({ task, onPause, onResume, onCancel, onRetry }: TransferCardProps) {
  const speedBytesPerSec = useTransferStore((state) => state.speedByTaskId[task.id]);
  const etaSeconds = useTransferStore((state) => state.etaByTaskId[task.id]);

  const isFailed = task.status === 'failed';
  const isPaused = task.status === 'paused';
  const percent = getProgressPercent(task);
  const progressVariant = task.type === 'upload' ? 'upload' : 'download';

  return (
    <div
      className={cn(
        'flex flex-col gap-2 rounded border border-border bg-bg-secondary p-4',
        'transition-colors duration-fast hover:border-accent',
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn('shrink-0 text-2xs font-semibold uppercase', getTypeTagClasses(task))}>
            {getTypeLabel(task)}
          </span>
          <span className="truncate text-sm font-medium text-fg-primary" title={getTransferDisplayName(task)}>
            {getTransferDisplayName(task)}
          </span>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {isFailed ? (
            <Tooltip content="Повторить">
              <Button variant="secondary" iconOnly aria-label="Повторить" onClick={() => onRetry(task.id)}>
                <RotateCcw className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Tooltip>
          ) : isPaused ? (
            <Tooltip content="Возобновить">
              <Button variant="secondary" iconOnly aria-label="Возобновить" onClick={() => onResume(task.id)}>
                <Play className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Tooltip>
          ) : (
            <Tooltip content="Пауза">
              <Button variant="secondary" iconOnly aria-label="Пауза" onClick={() => onPause(task.id)}>
                <Pause className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Tooltip>
          )}
          <Tooltip content="Отменить">
            <Button variant="secondary" iconOnly aria-label="Отменить" onClick={() => onCancel(task.id)}>
              <X className="h-4 w-4" aria-hidden="true" />
            </Button>
          </Tooltip>
        </div>
      </div>

      <p className="truncate font-mono text-xs text-fg-secondary" title={getTransferPathLine(task)}>
        {getTransferPathLine(task)}
      </p>

      {isFailed ? (
        <p className="truncate text-2xs text-danger" title={task.errorMessage || undefined}>
          {task.errorMessage || 'Не удалось выполнить передачу'}
        </p>
      ) : (
        <>
          <ProgressBar value={percent} variant={progressVariant} />
          <div className="flex items-center justify-between gap-2">
            <span className="text-[13px] font-semibold text-fg-primary">{percent}%</span>
            <div className="flex shrink-0 items-center gap-2 text-2xs text-fg-secondary">
              {!isPaused && speedBytesPerSec !== undefined && speedBytesPerSec > 0 && (
                <span>{formatSpeed(speedBytesPerSec)}</span>
              )}
              {!isPaused && etaSeconds !== undefined && <span>{formatETA(etaSeconds)}</span>}
              {isPaused && <span>—</span>}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
