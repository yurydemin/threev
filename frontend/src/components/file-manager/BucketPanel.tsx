import { useState } from 'react';
import { Database, AlertTriangle, Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { ContextMenu } from '../ui/ContextMenu';
import { Button } from '../ui/Button';
import { Tooltip } from '../ui/Tooltip';
import { CreateBucketModal } from './CreateBucketModal';
import { DeleteBucketConfirmModal } from './DeleteBucketConfirmModal';

const SKELETON_ROWS = 5;

/** Local shape for the currently open ПКМ context menu on a bucket row (`null` = hidden). */
interface BucketContextMenuState {
  x: number;
  y: number;
  bucketName: string;
}

/**
 * Second, narrower navigation panel for the File Manager, per Architectural
 * Decision 6 of the Stage 2 plan (reuses the "main nav 240px + contextual
 * 200px sub-panel" pattern from docs/03-ux-ui-spec.md section 5.7).
 *
 * Replaces the "Профили" + "Бакеты" sections from UX-spec section 5.4.2:
 * the profile switcher is dropped entirely (the main nav's "Подключения"
 * item, already wired in Block F, covers that), leaving just the active
 * profile's name as a header and its bucket list below.
 *
 * Reads `useFileManagerStore` directly rather than taking props — it has no
 * meaningful state or behavior independent of that store.
 */
export function BucketPanel() {
  const { t } = useTranslation();
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const activeProfileName = useFileManagerStore((state) => state.activeProfileName);
  const buckets = useFileManagerStore((state) => state.buckets);
  const isLoadingBuckets = useFileManagerStore((state) => state.isLoadingBuckets);
  const bucketsError = useFileManagerStore((state) => state.bucketsError);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const selectBucket = useFileManagerStore((state) => state.selectBucket);
  const enterProfile = useFileManagerStore((state) => state.enterProfile);

  const [contextMenu, setContextMenu] = useState<BucketContextMenuState | null>(null);
  const [isCreateBucketOpen, setIsCreateBucketOpen] = useState(false);
  const [deleteBucketTarget, setDeleteBucketTarget] = useState<string | null>(null);

  function handleRetry() {
    if (activeProfileId === null || activeProfileName === null) return;
    enterProfile(activeProfileId, activeProfileName);
  }

  return (
    <aside className="flex h-full w-[210px] shrink-0 flex-col border-r border-border bg-bg-secondary">
      <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border p-4">
        <p className="truncate text-[13px] font-semibold text-fg-primary" title={activeProfileName ?? ''}>
          {activeProfileName}
        </p>
        <Tooltip content={t('fileManager.bucketPanel.createBucket')}>
          <Button
            iconOnly
            variant="ghost"
            className="h-6 w-6 shrink-0"
            aria-label={t('fileManager.bucketPanel.createBucket')}
            disabled={activeProfileId === null || isLoadingBuckets || bucketsError !== null}
            onClick={() => setIsCreateBucketOpen(true)}
          >
            <Plus className="h-3.5 w-3.5" aria-hidden="true" />
          </Button>
        </Tooltip>
      </div>

      <nav className="flex flex-1 flex-col overflow-y-auto py-2">
        {isLoadingBuckets ? (
          Array.from({ length: SKELETON_ROWS }).map((_, index) => (
            // eslint-disable-next-line react/no-array-index-key
            <div key={index} className="flex h-9 items-center px-3">
              <div className="h-3.5 w-full animate-pulse-slow rounded-sm bg-bg-tertiary" />
            </div>
          ))
        ) : bucketsError ? (
          <div className="flex flex-col items-start gap-2 px-3 py-2">
            <div className="flex items-start gap-2 text-danger">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
              <p className="text-xs">{bucketsError}</p>
            </div>
            <button
              type="button"
              onClick={handleRetry}
              className="text-xs font-medium text-accent hover:underline"
            >
              {t('common.retry')}
            </button>
          </div>
        ) : buckets.length === 0 ? (
          <p className="px-3 py-2 text-xs text-fg-muted">{t('fileManager.bucketPanel.empty')}</p>
        ) : (
          buckets.map((bucket) => {
            const active = selectedBucket === bucket.name;
            return (
              <button
                key={bucket.name}
                type="button"
                onClick={() => selectBucket(bucket.name)}
                onContextMenu={(event) => {
                  event.preventDefault();
                  setContextMenu({ x: event.clientX, y: event.clientY, bucketName: bucket.name });
                }}
                title={bucket.name}
                className={cn(
                  'flex h-9 items-center gap-2 border-l-2 px-3 text-left text-[13px] transition-colors duration-fast',
                  active
                    ? 'border-accent bg-accent-subtle text-accent'
                    : 'border-transparent text-fg-secondary hover:bg-bg-tertiary',
                )}
              >
                <Database className="h-4 w-4 shrink-0" aria-hidden="true" />
                <span className="truncate">{bucket.name}</span>
              </button>
            );
          })
        )}
      </nav>

      {contextMenu && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          onClose={() => setContextMenu(null)}
          items={[
            {
              label: t('fileManager.bucketPanel.deleteBucket'),
              destructive: true,
              onClick: () => setDeleteBucketTarget(contextMenu.bucketName),
            },
          ]}
        />
      )}

      {activeProfileId !== null && (
        <CreateBucketModal
          isOpen={isCreateBucketOpen}
          onClose={() => setIsCreateBucketOpen(false)}
          profileId={activeProfileId}
        />
      )}

      {activeProfileId !== null && deleteBucketTarget !== null && (
        <DeleteBucketConfirmModal
          isOpen
          onClose={() => setDeleteBucketTarget(null)}
          profileId={activeProfileId}
          bucketName={deleteBucketTarget}
        />
      )}
    </aside>
  );
}
