import { useState } from 'react';
import {
  ChevronLeft,
  ChevronRight,
  CopyPlus,
  Download,
  FileUp,
  FolderInput,
  FolderPlus,
  FolderUp,
  LayoutGrid,
  List,
  RotateCcw,
  Search,
  Trash2,
  Upload,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { useTransferStore } from '../../stores/useTransferStore';
import { pickUploadDirectory } from '../../lib/wails/transfer';
import { pickAndQueueUploadFiles } from '../../lib/uploadFiles';
import { downloadSelectedObjects } from '../../lib/downloadSelected';
import { toast } from '../../lib/toast';
import { ApiError } from '../../lib/wails/errors';
import { Button } from '../ui/Button';
import { Tooltip } from '../ui/Tooltip';
import { ContextMenu, type ContextMenuItem } from '../ui/ContextMenu';
import { Breadcrumbs } from '../file-manager/Breadcrumbs';
import { CreateFolderModal } from '../file-manager/CreateFolderModal';

export type FileManagerView = 'list' | 'grid';

export interface ToolbarProps {
  /**
   * List/grid preference. Kept as parent (`FileManagerScreen`) local state
   * rather than in `useFileManagerStore`: it is a pure display preference
   * with no bearing on navigation/data (unlike `sortBy`/`searchQuery`, it
   * never needs to survive a `reset()` back to Connections, nor does any
   * other component need to read it), so folding it into the session store
   * would only add noise. If a future stage wants it to persist across
   * sessions, `useAppStore` (already persisted, e.g. `sidebarCollapsed`)
   * is the more natural home than the file-manager session store.
   */
  view: FileManagerView;
  onViewChange: (view: FileManagerView) => void;
  /** Opens `DestinationPickerModal` in copy mode for the given keys. */
  onBulkCopy: (keys: string[]) => void;
  /** Opens `DestinationPickerModal` in move mode for the given keys. */
  onBulkMove: (keys: string[]) => void;
  /** Opens `DeleteConfirmModal` for the given keys. */
  onBulkDelete: (keys: string[]) => void;
}

/**
 * Toolbar per docs/03-ux-ui-spec.md section 5.4.1.
 *
 * Reads navigation state (`history`, `selectedBucket`, `currentPrefix`,
 * `searchQuery`) directly from `useFileManagerStore` — like `BucketPanel`,
 * it has no meaningful behavior independent of that store.
 */
export function Toolbar({ view, onViewChange, onBulkCopy, onBulkMove, onBulkDelete }: ToolbarProps) {
  const { t } = useTranslation();
  const history = useFileManagerStore((state) => state.history);
  const historyIndex = useFileManagerStore((state) => state.historyIndex);
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);
  const selectedKeys = useFileManagerStore((state) => state.selectedKeys);
  const goBack = useFileManagerStore((state) => state.goBack);
  const goForward = useFileManagerStore((state) => state.goForward);
  const refresh = useFileManagerStore((state) => state.refresh);
  const navigateToPrefix = useFileManagerStore((state) => state.navigateToPrefix);
  const setSearchQuery = useFileManagerStore((state) => state.setSearchQuery);

  const [uploadMenu, setUploadMenu] = useState<{ x: number; y: number } | null>(null);
  const [isCreateFolderOpen, setIsCreateFolderOpen] = useState(false);

  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < history.length - 1;

  async function handlePickDirectory() {
    if (!activeProfileId || !selectedBucket) return;
    try {
      const path = await pickUploadDirectory();
      if (!path) return;
      await useTransferStore
        .getState()
        .queueUploadPaths(activeProfileId, selectedBucket, currentPrefix, [path]);
    } catch (err) {
      console.error('[Toolbar] pickUploadDirectory failed:', err);
      toast.error(
        err instanceof ApiError ? err.message : t('fileManager.toolbar.pickFolderError'),
        err instanceof ApiError ? err.raw : undefined,
      );
    }
  }

  const uploadMenuItems: ContextMenuItem[] = [
    {
      label: t('fileManager.toolbar.pickFiles'),
      icon: <FileUp className="h-4 w-4" aria-hidden="true" />,
      onClick: () => void pickAndQueueUploadFiles(activeProfileId, selectedBucket, currentPrefix),
    },
    {
      label: t('fileManager.toolbar.pickFolder'),
      icon: <FolderUp className="h-4 w-4" aria-hidden="true" />,
      onClick: () => void handlePickDirectory(),
    },
  ];

  return (
    <div className="flex h-header shrink-0 items-center justify-between gap-4 border-b border-border bg-bg-secondary px-4">
      <div className="flex min-w-0 items-center gap-2">
        <Tooltip content={t('fileManager.toolbar.back')}>
          <Button
            iconOnly
            variant="ghost"
            disabled={!canGoBack}
            onClick={() => goBack()}
            aria-label={t('fileManager.toolbar.back')}
          >
            <ChevronLeft className="h-5 w-5" aria-hidden="true" />
          </Button>
        </Tooltip>
        <Tooltip content={t('fileManager.toolbar.forward')}>
          <Button
            iconOnly
            variant="ghost"
            disabled={!canGoForward}
            onClick={() => goForward()}
            aria-label={t('fileManager.toolbar.forward')}
          >
            <ChevronRight className="h-5 w-5" aria-hidden="true" />
          </Button>
        </Tooltip>
        <Tooltip content={t('fileManager.toolbar.refresh')}>
          <Button iconOnly variant="ghost" onClick={() => refresh()} aria-label={t('fileManager.toolbar.refresh')}>
            <RotateCcw className="h-5 w-5" aria-hidden="true" />
          </Button>
        </Tooltip>

        <div className="mx-1 h-6 w-px shrink-0 bg-border" aria-hidden="true" />

        {selectedBucket && (
          <Breadcrumbs bucket={selectedBucket} prefix={currentPrefix} onNavigate={navigateToPrefix} />
        )}
      </div>

      <div className="flex shrink-0 items-center gap-2">
        <div className="relative">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-muted"
            aria-hidden="true"
          />
          <input
            type="search"
            value={searchQuery}
            onChange={(event) => setSearchQuery(event.target.value)}
            placeholder={t('fileManager.toolbar.searchPlaceholder')}
            className={cn(
              'h-8 rounded border border-border bg-bg-secondary pl-8 pr-2.5 text-[13px] text-fg-primary',
              'placeholder:text-fg-muted transition-[width,border-color] duration-fast',
              'focus:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-subtle',
              'w-[200px] focus:w-[280px]',
            )}
          />
        </div>

        <div className="flex items-center gap-0.5 rounded-sm border border-border p-0.5">
          <Tooltip content={t('fileManager.toolbar.listView')}>
            <Button
              iconOnly
              variant={view === 'list' ? 'secondary' : 'ghost'}
              className={cn('h-7 w-7', view === 'list' && 'border-none bg-bg-tertiary')}
              onClick={() => onViewChange('list')}
              aria-label={t('fileManager.toolbar.listView')}
              aria-pressed={view === 'list'}
            >
              <List className="h-4 w-4" aria-hidden="true" />
            </Button>
          </Tooltip>
          <Tooltip content={t('fileManager.toolbar.gridView')}>
            <Button
              iconOnly
              variant={view === 'grid' ? 'secondary' : 'ghost'}
              className={cn('h-7 w-7', view === 'grid' && 'border-none bg-bg-tertiary')}
              onClick={() => onViewChange('grid')}
              aria-label={t('fileManager.toolbar.gridView')}
              aria-pressed={view === 'grid'}
            >
              <LayoutGrid className="h-4 w-4" aria-hidden="true" />
            </Button>
          </Tooltip>
        </div>

        {selectedKeys.size > 0 && (
          <>
            <div className="flex items-center gap-0.5">
              <Tooltip content={t('fileManager.toolbar.bulkDownload')}>
                <Button
                  iconOnly
                  variant="ghost"
                  disabled={!activeProfileId || !selectedBucket}
                  onClick={() => {
                    if (!activeProfileId || !selectedBucket) return;
                    void downloadSelectedObjects(activeProfileId, selectedBucket, Array.from(selectedKeys));
                  }}
                  aria-label={t('fileManager.toolbar.bulkDownload')}
                >
                  <Download className="h-5 w-5" aria-hidden="true" />
                </Button>
              </Tooltip>
              <Tooltip content={t('fileManager.toolbar.bulkCopy')}>
                <Button
                  iconOnly
                  variant="ghost"
                  disabled={!activeProfileId || !selectedBucket}
                  onClick={() => onBulkCopy(Array.from(selectedKeys))}
                  aria-label={t('fileManager.toolbar.bulkCopy')}
                >
                  <CopyPlus className="h-5 w-5" aria-hidden="true" />
                </Button>
              </Tooltip>
              <Tooltip content={t('fileManager.toolbar.bulkMove')}>
                <Button
                  iconOnly
                  variant="ghost"
                  disabled={!activeProfileId || !selectedBucket}
                  onClick={() => onBulkMove(Array.from(selectedKeys))}
                  aria-label={t('fileManager.toolbar.bulkMove')}
                >
                  <FolderInput className="h-5 w-5" aria-hidden="true" />
                </Button>
              </Tooltip>
              <Tooltip content={t('fileManager.toolbar.bulkDelete', { count: selectedKeys.size })}>
                <Button
                  iconOnly
                  variant="ghost"
                  disabled={!activeProfileId || !selectedBucket}
                  onClick={() => onBulkDelete(Array.from(selectedKeys))}
                  aria-label={t('fileManager.toolbar.bulkDelete', { count: selectedKeys.size })}
                >
                  <Trash2 className="h-5 w-5" aria-hidden="true" />
                </Button>
              </Tooltip>
            </div>

            <div className="mx-1 h-6 w-px shrink-0 bg-border" aria-hidden="true" />
          </>
        )}

        <Button
          variant="secondary"
          disabled={!selectedBucket}
          onClick={() => setIsCreateFolderOpen(true)}
        >
          <FolderPlus className="h-4 w-4" aria-hidden="true" />
          {t('fileManager.toolbar.createFolder')}
        </Button>

        <Button
          variant="primary"
          disabled={!selectedBucket}
          onClick={(event) => {
            const rect = event.currentTarget.getBoundingClientRect();
            setUploadMenu({ x: rect.left, y: rect.bottom + 4 });
          }}
        >
          <Upload className="h-4 w-4" aria-hidden="true" />
          {t('fileManager.toolbar.upload')}
        </Button>
      </div>

      {uploadMenu && (
        <ContextMenu
          x={uploadMenu.x}
          y={uploadMenu.y}
          items={uploadMenuItems}
          onClose={() => setUploadMenu(null)}
        />
      )}

      {activeProfileId && selectedBucket && (
        <CreateFolderModal
          isOpen={isCreateFolderOpen}
          onClose={() => setIsCreateFolderOpen(false)}
          profileId={activeProfileId}
          bucket={selectedBucket}
          prefix={currentPrefix}
        />
      )}
    </div>
  );
}
