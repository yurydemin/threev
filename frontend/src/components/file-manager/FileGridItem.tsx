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
}

/**
 * Grid tile, per docs/03-ux-ui-spec.md section 5.4.4. Real image thumbnails
 * (via presigned URL) are Block I — every entry shows its type `FileIcon`
 * for now, same as the list view (task instructions: "не усложняй").
 */
export function FileGridItem({
  entry,
  currentPrefix,
  onNavigateToFolder,
  onOpenFile,
  onContextMenu,
}: FileGridItemProps) {
  const name = getEntryDisplayName(entry.key, currentPrefix);

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
      onDoubleClick={handleDoubleClick}
      onContextMenu={handleContextMenu}
      title={name}
      className="flex cursor-default flex-col items-center gap-1.5 rounded p-2 text-center transition-colors duration-fast hover:bg-bg-tertiary"
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
