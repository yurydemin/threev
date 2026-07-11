import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation();
  const hasPausable = queue.some((task) => task.status === 'pending' || task.status === 'running');
  const hasResumable = queue.some((task) => task.status === 'paused');

  return (
    <div className="flex shrink-0 items-center gap-2 border-t border-border px-4 py-3">
      <Button variant="secondary" disabled={!hasPausable} onClick={onPauseAll}>
        {t('transfers.groupActions.pauseAll')}
      </Button>
      <Button variant="secondary" disabled={!hasResumable} onClick={onResumeAll}>
        {t('transfers.groupActions.resumeAll')}
      </Button>
      <Button variant="secondary" disabled={queue.length === 0} onClick={onCancelAll}>
        {t('transfers.groupActions.cancelAll')}
      </Button>
    </div>
  );
}
