import type { MouseEvent } from 'react';
import { cn, formatBytes, getEntryDisplayName } from '../../lib/utils';
import type { ObjectEntry } from '../../types';
import { FileIcon } from './FileIcon';

export interface FileGridItemProps {
  entry: ObjectEntry;
  currentPrefix: string;
  onNavigateToFolder: (prefix: string) => void;
  onOpenFile: (entry: ObjectEntry) => void;
  onContextMenu: (entry: ObjectEntry, x: number, y: number) => void;
  /** Whether `entry.key` is currently selected (Stage 4, Block C). Always `false` for folders. */
  isSelected: boolean;
  /** Toggles/extends selection for `entry.key` — inspects `event.shiftKey` to decide between `toggleSelect`/`selectRange`. */
  onToggleSelect: (key: string, event: MouseEvent) => void;
}

/**
 * Grid tile, per docs/03-ux-ui-spec.md section 5.4.4. Real image thumbnails
 * (via presigned URL) are Block I — every entry shows its type `FileIcon`
 * for now, same as the list view (task instructions: "не усложняй").
 *
 * No dedicated checkbox (per spec 5.4.4) — a single click on a non-folder
 * tile toggles/extends selection; double-click still navigates/opens as
 * before (both handlers coexist safely: a double-click fires `onClick`
 * twice back-to-back, which is a selection no-op, then `onDoubleClick`).
 */
export function FileGridItem({
  entry,
  currentPrefix,
  onNavigateToFolder,
  onOpenFile,
  onContextMenu,
  isSelected,
  onToggleSelect,
}: FileGridItemProps) {
  const name = getEntryDisplayName(entry.key, currentPrefix);

  function handleClick(event: MouseEvent<HTMLDivElement>) {
    if (entry.isFolder) return;
    onToggleSelect(entry.key, event);
  }

  function handleDoubleClick() {
    if (entry.isFolder) {
      onNavigateToFolder(entry.key);
    } else {
      onOpenFile(entry);
    }
  }

  function handleContextMenu(event: MouseEvent<HTMLDivElement>) {
    event.preventDefault();
    onContextMenu(entry, event.clientX, event.clientY);
  }

  return (
    <div
      onClick={handleClick}
      onDoubleClick={handleDoubleClick}
      onContextMenu={handleContextMenu}
      title={name}
      className={cn(
        'flex cursor-default flex-col items-center gap-1.5 rounded border p-2 text-center transition-colors duration-fast hover:bg-bg-tertiary',
        isSelected ? 'border-accent bg-accent-subtle' : 'border-transparent',
      )}
    >
      <FileIcon isFolder={entry.isFolder} contentType={entry.contentType} size={48} />
      <span
        className={cn(
          'line-clamp-2 w-full break-words text-xs leading-tight text-fg-primary',
          entry.isFolder && 'font-medium',
        )}
      >
        {name}
        {entry.isFolder && '/'}
      </span>
      {!entry.isFolder && <span className="text-2xs text-fg-muted">{formatBytes(entry.size)}</span>}
    </div>
  );
}
