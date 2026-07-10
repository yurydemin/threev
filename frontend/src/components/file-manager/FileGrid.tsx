import type { MouseEvent } from 'react';
import { Folder as FolderIcon } from 'lucide-react';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { pickAndQueueUploadFiles } from '../../lib/uploadFiles';
import type { ObjectEntry } from '../../types';
import { Button } from '../ui/Button';
import { FileGridItem } from './FileGridItem';

const SKELETON_TILES = 10;

export interface FileGridProps {
  /** Entries already filtered by `searchQuery` — see `FileList` for the rationale. */
  entries: ObjectEntry[];
  onOpenFile: (entry: ObjectEntry) => void;
  onContextMenu: (entry: ObjectEntry, x: number, y: number) => void;
}

/**
 * Grid view of the Object List, per docs/03-ux-ui-spec.md section 5.4.4.
 * Mirrors `FileList`'s state-reading conventions and loading/empty states.
 */
export function FileGrid({ entries, onOpenFile, onContextMenu }: FileGridProps) {
  const rawEntryCount = useFileManagerStore((state) => state.entries.length);
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);
  const isLoadingEntries = useFileManagerStore((state) => state.isLoadingEntries);
  const entriesError = useFileManagerStore((state) => state.entriesError);
  const isTruncated = useFileManagerStore((state) => state.isTruncated);
  const loadMore = useFileManagerStore((state) => state.loadMore);
  const navigateToPrefix = useFileManagerStore((state) => state.navigateToPrefix);
  const selectedKeys = useFileManagerStore((state) => state.selectedKeys);
  const toggleSelect = useFileManagerStore((state) => state.toggleSelect);
  const selectRange = useFileManagerStore((state) => state.selectRange);

  function handleToggleSelect(key: string, event: MouseEvent) {
    if (event.shiftKey) {
      selectRange(key);
    } else {
      toggleSelect(key);
    }
  }

  const isInitialLoading = isLoadingEntries && rawEntryCount === 0;

  if (isInitialLoading) {
    return (
      <div className="grid grid-cols-[repeat(auto-fill,minmax(120px,1fr))] gap-3 overflow-y-auto p-4">
        {Array.from({ length: SKELETON_TILES }).map((_, index) => (
          // eslint-disable-next-line react/no-array-index-key
          <div key={index} className="flex flex-col items-center gap-1.5 p-2">
            <div className="h-12 w-12 animate-pulse-slow rounded bg-bg-tertiary" />
            <div className="h-3 w-3/4 animate-pulse-slow rounded-sm bg-bg-tertiary" />
          </div>
        ))}
      </div>
    );
  }

  if (entriesError) {
    return <p className="px-4 py-6 text-center text-sm text-danger">{entriesError}</p>;
  }

  if (entries.length === 0) {
    const hasSearchQuery = searchQuery.trim().length > 0;
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 py-16 text-center">
        <FolderIcon className="h-12 w-12 text-fg-muted" aria-hidden="true" />
        <p className="text-sm text-fg-primary">{hasSearchQuery ? 'Ничего не найдено' : 'Эта папка пуста'}</p>
        {!hasSearchQuery && (
          <Button
            variant="primary"
            className="mt-2"
            onClick={() => void pickAndQueueUploadFiles(activeProfileId, selectedBucket, currentPrefix)}
          >
            Загрузить файлы
          </Button>
        )}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto p-4">
      <div className="grid grid-cols-[repeat(auto-fill,minmax(120px,1fr))] gap-3">
        {entries.map((entry) => (
          <FileGridItem
            key={entry.key}
            entry={entry}
            currentPrefix={currentPrefix}
            onNavigateToFolder={navigateToPrefix}
            onOpenFile={onOpenFile}
            onContextMenu={onContextMenu}
            isSelected={selectedKeys.has(entry.key)}
            onToggleSelect={handleToggleSelect}
          />
        ))}
      </div>
      {isTruncated && (
        <div className="flex justify-center py-4">
          <Button variant="secondary" isLoading={isLoadingEntries} onClick={() => loadMore()}>
            Загрузить ещё
          </Button>
        </div>
      )}
    </div>
  );
}
