import { useMemo, useState } from 'react';
import { Sidebar } from '../components/layout/Sidebar';
import { Toolbar, type FileManagerView } from '../components/layout/Toolbar';
import { StatusBar } from '../components/layout/StatusBar';
import { BucketPanel } from '../components/file-manager/BucketPanel';
import { FileList } from '../components/file-manager/FileList';
import { FileGrid } from '../components/file-manager/FileGrid';
import { useFileManagerStore } from '../stores/useFileManagerStore';
import { filterEntriesByQuery } from '../lib/utils';
import type { ObjectEntry } from '../types';

export interface FileManagerScreenProps {
  profileId: number;
  profileName: string;
  /** Returns to the Connections screen (also resets `useFileManagerStore`). */
  onExit: () => void;
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
 * `onOpenFile`/`onContextMenu` are still stubs — real preview dispatch and
 * the ПКМ context menu are Block I, the next step.
 */
export function FileManagerScreen({ profileId, profileName, onExit }: FileManagerScreenProps) {
  const [view, setView] = useState<FileManagerView>('list');
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const entries = useFileManagerStore((state) => state.entries);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);

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
    // TODO(Block I): dispatch to ObjectPreviewModal (image/pdf/text).
    console.log('[FileManagerScreen] open file (Block I not implemented yet):', entry.key);
  }

  function handleContextMenu(entry: ObjectEntry, x: number, y: number) {
    // TODO(Block I): show ObjectContextMenu at (x, y).
    console.log('[FileManagerScreen] context menu (Block I not implemented yet):', entry.key, x, y);
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar onSelectConnections={onExit} />
      <BucketPanel />

      <div className="flex min-w-0 flex-1 flex-col">
        <Toolbar view={view} onViewChange={setView} />

        <main className="flex min-h-0 flex-1 flex-col overflow-hidden">
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
        </main>

        <StatusBar left={statusLeft} />
      </div>
    </div>
  );
}
