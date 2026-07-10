import { useMemo, useState, type CSSProperties } from 'react';
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
import { useFileManagerStore } from '../stores/useFileManagerStore';
import { useTransferStore } from '../stores/useTransferStore';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';
import { useFileDropUpload } from '../hooks/useFileDropUpload';
import { filterEntriesByQuery } from '../lib/utils';
import { isPreviewSupported } from '../lib/preview';
import type { ObjectEntry } from '../types';

/** Local shape for the currently open ПКМ context menu (`null` = hidden). */
interface ContextMenuState {
  entry: ObjectEntry;
  x: number;
  y: number;
}

export interface FileManagerScreenProps {
  profileId: number;
  profileName: string;
  /** Returns to the Connections screen (also resets `useFileManagerStore`). */
  onExit: () => void;
  /** Navigates to the Transfers screen (Sidebar "Передачи" and the `StatusBar` transfer indicator). */
  onSelectTransfers: () => void;
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
export function FileManagerScreen({ profileId, profileName, onExit, onSelectTransfers }: FileManagerScreenProps) {
  const [view, setView] = useState<FileManagerView>('list');
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [previewEntry, setPreviewEntry] = useState<ObjectEntry | null>(null);
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const entries = useFileManagerStore((state) => state.entries);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);
  const refresh = useFileManagerStore((state) => state.refresh);
  const queueCount = useTransferStore((state) => state.queue.length);

  useKeyboardShortcuts({ onRefresh: refresh });

  const { isDraggingOver, dragHandlers } = useFileDropUpload(activeProfileId, selectedBucket, currentPrefix);

  const filteredEntries = useMemo(
    () => filterEntriesByQuery(entries, searchQuery, currentPrefix),
    [entries, searchQuery, currentPrefix],
  );

  const hasSearchQuery = searchQuery.trim().length > 0;
  const statusLeft = selectedBucket
    ? hasSearchQuery
      ? `${filteredEntries.length} из ${entries.length} объектов`
      : `${entries.length} объектов`
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
      <Sidebar onSelectConnections={onExit} onSelectTransfers={onSelectTransfers} />
      <BucketPanel />

      <div className="flex min-w-0 flex-1 flex-col">
        <Toolbar view={view} onViewChange={setView} />

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
                Выберите бакет слева, чтобы просмотреть его содержимое
              </p>
            </div>
          )}
          {isDraggingOver && <DropOverlay />}
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
        />
      )}

      <ObjectPreviewModal
        entry={previewEntry}
        isOpen={previewEntry !== null}
        onClose={() => setPreviewEntry(null)}
      />
    </div>
  );
}
