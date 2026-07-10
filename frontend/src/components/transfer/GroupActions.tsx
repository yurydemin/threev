import { Button } from '../ui/Button';
import type { TransferTask } from '../../types';

export interface GroupActionsProps {
  queue: TransferTask[];
  onPauseAll: () => void;
  onResumeAll: () => void;
  onCancelAll: () => void;
}

/**
 * "Пауза все" / "Возобновить все" / "Отменить все", per
 * docs/03-ux-ui-spec.md section 5.5. Only rendered by `TransferScreen` on
 * the "Активные" tab (the spec's mockup places it there) - this component
 * itself has no opinion on the current tab, it just derives its disabled
 * states from `queue`.
 */
export function GroupActions({ queue, onPauseAll, onResumeAll, onCancelAll }: GroupActionsProps) {
  const hasPausable = queue.some((task) => task.status === 'pending' || task.status === 'running');
  const hasResumable = queue.some((task) => task.status === 'paused');

  return (
    <div className="flex shrink-0 items-center gap-2 border-t border-border px-4 py-3">
      <Button variant="secondary" disabled={!hasPausable} onClick={onPauseAll}>
        Пауза все
      </Button>
      <Button variant="secondary" disabled={!hasResumable} onClick={onResumeAll}>
        Возобновить все
      </Button>
      <Button variant="secondary" disabled={queue.length === 0} onClick={onCancelAll}>
        Отменить все
      </Button>
    </div>
  );
}
