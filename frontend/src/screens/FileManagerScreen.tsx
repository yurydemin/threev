import { useMemo, useState, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { Loader2 } from 'lucide-react';
import { Sidebar } from '../components/layout/Sidebar';
import { Toolbar, type FileManagerView } from '../components/layout/Toolbar';
import { StatusBar } from '../components/layout/StatusBar';
import { TransferIndicator } from '../components/layout/TransferIndicator';
import { BucketPanel } from '../components/file-manager/BucketPanel';
import { FileList } from '../components/file-manager/FileList';
import { FileGrid } from '../components/file-manager/FileGrid';
import { ObjectContextMenu } from '../components/file-manager/ObjectContextMenu';
import { ObjectPreviewModal } from '../components/file-manager/ObjectPreviewModal';
import { DropOverlay } from '../components/file-manager/DropOverlay';
import { BulkProgressOverlay } from '../components/file-manager/BulkProgressOverlay';
import { DeleteConfirmModal } from '../components/file-manager/DeleteConfirmModal';
import { DestinationPickerModal } from '../components/file-manager/DestinationPickerModal';
import { RenameModal } from '../components/file-manager/RenameModal';
import { PropertiesModal } from '../components/file-manager/PropertiesModal';
import { PresignedUrlModal } from '../components/file-manager/PresignedUrlModal';
import { useFileManagerStore } from '../stores/useFileManagerStore';
import { useTransferStore } from '../stores/useTransferStore';
import { useBulkOperationStore } from '../stores/useBulkOperationStore';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';
import { useFileDropUpload } from '../hooks/useFileDropUpload';
import { useBulkOperationEvents } from '../hooks/useBulkOperationEvents';
import { filterEntriesByQuery, getEntryDisplayName } from '../lib/utils';
import { isPreviewSupported } from '../lib/preview';
import { listAllKeysUnderPrefix } from '../lib/wails/fileManager';
import { ApiError } from '../lib/wails/errors';
import { toast } from '../lib/toast';
import type { Favorite, ObjectEntry } from '../types';

/** Local shape for the currently open ПКМ context menu (`null` = hidden). */
interface ContextMenuState {
  entry: ObjectEntry;
  x: number;
  y: number;
}

/**
 * Local shape for the currently open bulk/single-object modal (`null` =
 * none open). A single union rather than one boolean flag per modal: at
 * most one of these is ever meaningfully open at a time, so a union keeps
 * "which modal, with what payload" as one piece of state instead of six
 * booleans plus six separately-tracked payloads that would need to stay in
 * sync with each other.
 */
type ActiveModalState =
  | { kind: 'delete'; keys: string[]; folderName?: string }
  | { kind: 'copy'; keys: string[] }
  | { kind: 'move'; keys: string[] }
  | { kind: 'rename'; entry: ObjectEntry }
  | { kind: 'metadata'; entry: ObjectEntry }
  | { kind: 'presignedUrl'; entry: ObjectEntry }
  | null;

export interface FileManagerScreenProps {
  profileId: number;
  profileName: string;
  /** Navigates to the Connections screen (Sidebar "Подключения") — a plain navigation, no `useFileManagerStore` reset (Stage 4 Block L5). */
  onSelectConnections: () => void;
  /** Navigates to the Transfers screen (Sidebar "Передачи" and the `StatusBar` transfer indicator). */
  onSelectTransfers: () => void;
  /** Navigates to the History screen (Sidebar "История"). */
  onSelectHistory: () => void;
  /** Navigates to the Settings screen (Sidebar "Настройки"). */
  onSelectSettings: () => void;
  /** Closes this File Manager session (Sidebar active-connection indicator's "X" button). */
  onDisconnect: () => void;
  /** Handles a click on a Sidebar favorites-section row. */
  onSelectFavorite: (favorite: Favorite) => void;
}

/**
 * File Manager screen — layout shell (Stage 2, Block G) + Object List
 * (Stage 2, Block H). Per Architectural Decision 6 of the Stage 2 plan.
 *
 * `searchQuery` filtering happens here (not in the store, not duplicated in
 * `FileList`/`FileGrid`): it's the one piece of derived state both the
 * list/grid view *and* the `StatusBar` count need, so computing it once and
 * threading it down keeps the two in sync for free.
 *
 * `onOpenFile` opens `ObjectPreviewModal` for files whose type
 * `lib/preview.ts#isPreviewSupported` recognizes, and is a no-op for
 * everything else (folders are already handled by `FileRow`/`FileGridItem`
 * themselves via `onNavigateToFolder`; a file without a supported preview
 * has no Stage 2 action to fall back to — see Block I task notes — so a
 * double-click on e.g. a `.zip` simply does nothing rather than showing an
 * empty/broken modal). `onContextMenu` opens `ObjectContextMenu` at the
 * click position.
 */
export function FileManagerScreen({
  profileId,
  profileName,
  onSelectConnections,
  onSelectTransfers,
  onSelectHistory,
  onSelectSettings,
  onDisconnect,
  onSelectFavorite,
}: FileManagerScreenProps) {
  const { t } = useTranslation();
  const [view, setView] = useState<FileManagerView>('list');
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [previewEntry, setPreviewEntry] = useState<ObjectEntry | null>(null);
  const [activeModal, setActiveModal] = useState<ActiveModalState>(null);
  const [isListingFolder, setIsListingFolder] = useState(false);
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const entries = useFileManagerStore((state) => state.entries);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);
  const refresh = useFileManagerStore((state) => state.refresh);
  const selectedKeys = useFileManagerStore((state) => state.selectedKeys);
  const selectAll = useFileManagerStore((state) => state.selectAll);
  const clearSelection = useFileManagerStore((state) => state.clearSelection);
  const queueCount = useTransferStore((state) => state.queue.length);

  useBulkOperationEvents();

  function handleDeleteSelected() {
    if (selectedKeys.size === 0) return;
    setActiveModal({ kind: 'delete', keys: Array.from(selectedKeys) });
  }

  /**
   * "Удалить" on a folder in `ObjectContextMenu` — S3 has no delete-by-prefix,
   * so this first recursively walks every real key under `entry.key`
   * (`listAllKeysUnderPrefix`, backend `ListAllKeysUnderPrefix`) before
   * opening the existing delete confirmation flow with the full key list.
   * An empty result (a folder with no real objects underneath, only its own
   * placeholder key never existed or was already the sole key) reuses the
   * plain single-key delete path unchanged — there's nothing "recursive" to
   * warn about in that case.
   */
  async function handleDeleteFolder(entry: ObjectEntry) {
    if (!activeProfileId || !selectedBucket) return;
    setIsListingFolder(true);
    try {
      const keys = await listAllKeysUnderPrefix(activeProfileId, selectedBucket, entry.key);
      if (keys.length === 0) {
        setActiveModal({ kind: 'delete', keys: [entry.key] });
        return;
      }
      setActiveModal({ kind: 'delete', keys, folderName: getEntryDisplayName(entry.key, currentPrefix) });
    } catch (err) {
      console.error('[FileManagerScreen] listAllKeysUnderPrefix failed:', err);
      toast.error(
        err instanceof ApiError ? err.message : t('fileManager.deleteConfirmModal.listFolderError'),
        err instanceof ApiError ? err.raw : undefined,
      );
    } finally {
      setIsListingFolder(false);
    }
  }

  function handleRenameSelected() {
    if (selectedKeys.size !== 1) return;
    const [key] = Array.from(selectedKeys);
    const entry = entries.find((candidate) => candidate.key === key);
    if (entry) setActiveModal({ kind: 'rename', entry });
  }

  useKeyboardShortcuts({
    onRefresh: refresh,
    onSelectAll: selectAll,
    onClearSelection: clearSelection,
    onDeleteSelected: handleDeleteSelected,
    onRenameSelected: handleRenameSelected,
  });

  const { isDraggingOver, dragHandlers } = useFileDropUpload(activeProfileId, selectedBucket, currentPrefix);

  const filteredEntries = useMemo(
    () => filterEntriesByQuery(entries, searchQuery, currentPrefix),
    [entries, searchQuery, currentPrefix],
  );

  const hasSearchQuery = searchQuery.trim().length > 0;
  const selectionSuffix =
    selectedKeys.size > 0 ? t('fileManager.screen.statusSelectedSuffix', { count: selectedKeys.size }) : '';
  const statusLeft = selectedBucket
    ? (hasSearchQuery
        ? t('fileManager.screen.statusFiltered', {
            filtered: filteredEntries.length,
            total: entries.length,
            count: entries.length,
          })
        : t('fileManager.screen.statusAll', { total: entries.length, count: entries.length })) + selectionSuffix
    : undefined;

  function handleOpenFile(entry: ObjectEntry) {
    if (!isPreviewSupported(entry.contentType)) return;
    setPreviewEntry(entry);
  }

  function handleContextMenu(entry: ObjectEntry, x: number, y: number) {
    setContextMenu({ entry, x, y });
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar
        activeItem="fileManager"
        onSelectConnections={onSelectConnections}
        onSelectTransfers={onSelectTransfers}
        onSelectHistory={onSelectHistory}
        onSelectSettings={onSelectSettings}
        onDisconnect={onDisconnect}
        onSelectFavorite={onSelectFavorite}
      />
      <BucketPanel />

      <div className="flex min-w-0 flex-1 flex-col">
        <Toolbar
          view={view}
          onViewChange={setView}
          onBulkCopy={(keys) => setActiveModal({ kind: 'copy', keys })}
          onBulkMove={(keys) => setActiveModal({ kind: 'move', keys })}
          onBulkDelete={(keys) => setActiveModal({ kind: 'delete', keys })}
        />

        <main
          className="relative flex min-h-0 flex-1 flex-col overflow-hidden"
          // Required for Wails' `OnFileDrop(callback, true)` to recognize this
          // area as a valid drop target — see `useFileDropUpload` for why this
          // is independent from `dragHandlers` (which only drive the overlay).
          style={{ '--wails-drop-target': 'drop' } as CSSProperties}
          {...dragHandlers}
        >
          {selectedBucket ? (
            view === 'list' ? (
              <FileList entries={filteredEntries} onOpenFile={handleOpenFile} onContextMenu={handleContextMenu} />
            ) : (
              <FileGrid entries={filteredEntries} onOpenFile={handleOpenFile} onContextMenu={handleContextMenu} />
            )
          ) : (
            <div className="flex flex-1 items-center justify-center">
              <p className="text-sm text-fg-muted">
                {t('fileManager.screen.selectBucketHint')}
              </p>
            </div>
          )}
          {isDraggingOver && <DropOverlay />}
          <BulkProgressOverlay />
        </main>

        <StatusBar left={statusLeft} right={<TransferIndicator count={queueCount} onClick={onSelectTransfers} />} />
      </div>

      {contextMenu && (
        <ObjectContextMenu
          entry={contextMenu.entry}
          x={contextMenu.x}
          y={contextMenu.y}
          onClose={() => setContextMenu(null)}
          onOpenPreview={setPreviewEntry}
          onDelete={(keys) => setActiveModal({ kind: 'delete', keys })}
          onDeleteFolder={(entry) => void handleDeleteFolder(entry)}
          onCopy={(keys) => setActiveModal({ kind: 'copy', keys })}
          onMove={(keys) => setActiveModal({ kind: 'move', keys })}
          onRename={(entry) => setActiveModal({ kind: 'rename', entry })}
          onEditMetadata={(entry) => setActiveModal({ kind: 'metadata', entry })}
          onGetPresignedUrl={(entry) => setActiveModal({ kind: 'presignedUrl', entry })}
        />
      )}

      {isListingFolder && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/20">
          <div className="flex items-center gap-2 rounded bg-bg-elevated px-4 py-3 shadow-[0_4px_16px_rgba(0,0,0,0.20)]">
            <Loader2 className="h-4 w-4 animate-spin text-fg-primary" aria-hidden="true" />
            <span className="text-[13px] text-fg-primary">{t('fileManager.deleteConfirmModal.listingFolder')}</span>
          </div>
        </div>
      )}

      <ObjectPreviewModal
        entry={previewEntry}
        isOpen={previewEntry !== null}
        onClose={() => setPreviewEntry(null)}
      />

      {activeModal?.kind === 'delete' && activeProfileId && selectedBucket && (
        <DeleteConfirmModal
          isOpen
          onClose={() => setActiveModal(null)}
          keys={activeModal.keys}
          folderName={activeModal.folderName}
          onConfirm={() =>
            void useBulkOperationStore.getState().startDelete(activeProfileId, selectedBucket, activeModal.keys)
          }
        />
      )}

      {(activeModal?.kind === 'copy' || activeModal?.kind === 'move') && activeProfileId && selectedBucket && (
        <DestinationPickerModal
          isOpen
          onClose={() => setActiveModal(null)}
          mode={activeModal.kind}
          keys={activeModal.keys}
          profileId={activeProfileId}
          sourceBucket={selectedBucket}
          onConfirm={(destBucket, destPrefix) => {
            const { keys } = activeModal;
            if (activeModal.kind === 'copy') {
              void useBulkOperationStore
                .getState()
                .startCopy(activeProfileId, selectedBucket, keys, destBucket, destPrefix);
            } else {
              void useBulkOperationStore
                .getState()
                .startMove(activeProfileId, selectedBucket, keys, destBucket, destPrefix);
            }
          }}
        />
      )}

      {activeModal?.kind === 'rename' && activeProfileId && selectedBucket && (
        <RenameModal
          isOpen
          onClose={() => setActiveModal(null)}
          profileId={activeProfileId}
          bucket={selectedBucket}
          entry={activeModal.entry}
          currentPrefix={currentPrefix}
        />
      )}

      {activeModal?.kind === 'metadata' && activeProfileId && selectedBucket && (
        <PropertiesModal
          isOpen
          onClose={() => setActiveModal(null)}
          profileId={activeProfileId}
          bucket={selectedBucket}
          entry={activeModal.entry}
        />
      )}

      {activeModal?.kind === 'presignedUrl' && activeProfileId && selectedBucket && (
        <PresignedUrlModal
          isOpen
          onClose={() => setActiveModal(null)}
          profileId={activeProfileId}
          bucket={selectedBucket}
          objectKey={activeModal.entry.key}
        />
      )}
    </div>
  );
}
