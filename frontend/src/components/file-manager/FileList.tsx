import { ChevronDown, ChevronUp, Folder as FolderIcon } from 'lucide-react';
import { cn } from '../../lib/utils';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import type { ObjectEntry } from '../../types';
import { Button } from '../ui/Button';
import { FileRow } from './FileRow';

const SKELETON_ROWS = 5;

type SortColumn = 'name' | 'size' | 'type' | 'modified';

const COLUMNS: { key: SortColumn; label: string; flexClass: string; alignRight?: boolean }[] = [
  { key: 'name', label: 'Имя', flexClass: 'flex-[3]' },
  { key: 'size', label: 'Размер', flexClass: 'flex-1', alignRight: true },
  { key: 'type', label: 'Тип', flexClass: 'flex-1' },
  { key: 'modified', label: 'Изменён', flexClass: 'flex-1' },
];

export interface FileListProps {
  /**
   * Entries already filtered by `searchQuery` — computed once in
   * `FileManagerScreen` (shared with `StatusBar`'s "N из M" count) rather
   * than duplicated here and in `FileGrid`.
   */
  entries: ObjectEntry[];
  onOpenFile: (entry: ObjectEntry) => void;
  onContextMenu: (entry: ObjectEntry, x: number, y: number) => void;
}

/**
 * Table view of the Object List, per docs/03-ux-ui-spec.md section 5.4.3.
 * No checkbox/actions columns (Stage 2 constraint, see `FileRow`).
 *
 * Reads loading/error/sort/navigation state directly from
 * `useFileManagerStore` — same convention as `Toolbar`/`BucketPanel` — and
 * only takes `entries`/`onOpenFile`/`onContextMenu` as props (the first
 * because it's a derived, search-filtered view; the latter two because
 * Block I owns their real implementation).
 */
export function FileList({ entries, onOpenFile, onContextMenu }: FileListProps) {
  const rawEntryCount = useFileManagerStore((state) => state.entries.length);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);
  const sortBy = useFileManagerStore((state) => state.sortBy);
  const sortOrder = useFileManagerStore((state) => state.sortOrder);
  const isLoadingEntries = useFileManagerStore((state) => state.isLoadingEntries);
  const entriesError = useFileManagerStore((state) => state.entriesError);
  const isTruncated = useFileManagerStore((state) => state.isTruncated);
  const setSort = useFileManagerStore((state) => state.setSort);
  const loadMore = useFileManagerStore((state) => state.loadMore);
  const navigateToPrefix = useFileManagerStore((state) => state.navigateToPrefix);

  /**
   * Cycle for the clicked column: asc → desc → "none" (falls back to the
   * store's default sort, `name`/`asc` — the backend has no concept of an
   * unsorted listing, see `internal/filemanager/sort.go`, so "none" simply
   * means the indicator moves off this column back onto `name`).
   * Clicking a *different* column always starts it at `asc`.
   */
  function handleHeaderClick(column: SortColumn) {
    if (sortBy !== column) {
      setSort(column, 'asc');
    } else if (sortOrder === 'asc') {
      setSort(column, 'desc');
    } else {
      setSort('name', 'asc');
    }
  }

  const isInitialLoading = isLoadingEntries && rawEntryCount === 0;

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div
        role="row"
        className="flex h-row shrink-0 items-center border-b border-border bg-bg-secondary px-4 text-2xs font-semibold uppercase tracking-wide text-fg-muted"
      >
        {COLUMNS.map((column) => (
          <button
            key={column.key}
            type="button"
            role="columnheader"
            onClick={() => handleHeaderClick(column.key)}
            aria-sort={sortBy === column.key ? (sortOrder === 'desc' ? 'descending' : 'ascending') : 'none'}
            className={cn(
              'flex items-center gap-1 truncate transition-colors duration-fast hover:text-fg-primary',
              column.flexClass,
              column.alignRight ? 'justify-end pr-2 text-right' : 'pr-2 text-left',
            )}
          >
            <span>{column.label}</span>
            {sortBy === column.key &&
              (sortOrder === 'desc' ? (
                <ChevronDown className="h-3 w-3 shrink-0" aria-hidden="true" />
              ) : (
                <ChevronUp className="h-3 w-3 shrink-0" aria-hidden="true" />
              ))}
          </button>
        ))}
      </div>

      <div className="flex-1 overflow-y-auto">
        {isInitialLoading ? (
          Array.from({ length: SKELETON_ROWS }).map((_, index) => (
            // eslint-disable-next-line react/no-array-index-key
            <div key={index} className="flex h-row shrink-0 items-center gap-3 border-b border-border-subtle px-4">
              <div className="h-3.5 flex-[3] animate-pulse-slow rounded-sm bg-bg-tertiary" />
              <div className="h-3.5 flex-1 animate-pulse-slow rounded-sm bg-bg-tertiary" />
              <div className="h-3.5 flex-1 animate-pulse-slow rounded-sm bg-bg-tertiary" />
              <div className="h-3.5 flex-1 animate-pulse-slow rounded-sm bg-bg-tertiary" />
            </div>
          ))
        ) : entriesError ? (
          <p className="px-4 py-6 text-center text-sm text-danger">{entriesError}</p>
        ) : entries.length === 0 ? (
          <EmptyListState hasSearchQuery={searchQuery.trim().length > 0} />
        ) : (
          <>
            {entries.map((entry) => (
              <FileRow
                key={entry.key}
                entry={entry}
                currentPrefix={currentPrefix}
                onNavigateToFolder={navigateToPrefix}
                onOpenFile={onOpenFile}
                onContextMenu={onContextMenu}
              />
            ))}
            {isTruncated && (
              <div className="flex justify-center py-3">
                <Button variant="secondary" isLoading={isLoadingEntries} onClick={() => loadMore()}>
                  Загрузить ещё
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function EmptyListState({ hasSearchQuery }: { hasSearchQuery: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <FolderIcon className="h-12 w-12 text-fg-muted" aria-hidden="true" />
      <p className="text-sm text-fg-primary">{hasSearchQuery ? 'Ничего не найдено' : 'Эта папка пуста'}</p>
    </div>
  );
}
