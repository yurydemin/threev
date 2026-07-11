import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useBulkOperationStore } from '../../stores/useBulkOperationStore';
import { Button } from '../ui/Button';
import { ProgressBar } from '../ui/ProgressBar';

function getVerbByType(t: TFunction): Record<string, string> {
  return {
    delete: t('fileManager.bulkProgressOverlay.verbDelete'),
    copy: t('fileManager.bulkProgressOverlay.verbCopy'),
    move: t('fileManager.bulkProgressOverlay.verbMove'),
  };
}

/**
 * Inline progress row for the currently active bulk delete/copy/move
 * operation (`useBulkOperationStore`), per docs/03-ux-ui-spec.md section
 * 5.8.4. Rendered by `FileManagerScreen` inside the same `relative` `<main>`
 * container `DropOverlay` uses, as a top docked bar rather than a
 * full-screen overlay (`absolute top-0 left-0 right-0`, not `inset-0`) —
 * the object list stays visible/scrollable underneath while an operation
 * runs.
 *
 * Reads `active` from `useBulkOperationStore` directly (rather than via a
 * prop threaded through `FileManagerScreen`, unlike `DropOverlay`'s
 * `isDraggingOver`): it also needs the store's `cancel` action for its
 * button, so owning the read itself avoids threading both a value and a
 * callback down for the same store.
 *
 * Renders nothing when there is no active operation — same "render nothing"
 * convention as `TransferIndicator`.
 */
export function BulkProgressOverlay() {
  const { t } = useTranslation();
  const active = useBulkOperationStore((state) => state.active);

  if (!active) return null;

  const percent = active.total > 0 ? Math.round((active.completed / active.total) * 100) : 100;
  const verb = getVerbByType(t)[active.type] ?? t('fileManager.bulkProgressOverlay.verbFallback');

  return (
    <div className="absolute left-0 right-0 top-0 z-20 border-b border-border bg-bg-elevated px-4 py-2.5 shadow-[0_2px_8px_rgba(0,0,0,0.12)]">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] text-fg-primary">
            {t('fileManager.bulkProgressOverlay.progressLine', { verb, total: active.total })}
          </p>
          <ProgressBar value={percent} variant="upload" className="mt-1.5" />
          {active.failedCount > 0 && (
            <p className="mt-1 text-2xs text-danger">
              {t('fileManager.bulkProgressOverlay.failedCount', { count: active.failedCount })}
            </p>
          )}
        </div>
        <Button
          variant="secondary"
          disabled={active.status !== 'running'}
          onClick={() => void useBulkOperationStore.getState().cancel()}
        >
          {t('common.cancel')}
        </Button>
      </div>
    </div>
  );
}
