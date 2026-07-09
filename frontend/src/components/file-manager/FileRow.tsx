import type { MouseEvent } from 'react';
import { cn, formatBytes, getEntryDisplayName } from '../../lib/utils';
import type { ObjectEntry } from '../../types';
import { FileIcon } from './FileIcon';

export interface FileRowProps {
  entry: ObjectEntry;
  /** Current browsed prefix, used to derive the display name from `entry.key`. */
  currentPrefix: string;
  /** Called with `entry.key` (the folder's own prefix) on folder double-click. */
  onNavigateToFolder: (prefix: string) => void;
  /**
   * Called on file double-click. Real preview dispatch (image/pdf/text) is
   * Block I — this is a plain pass-through callback for now.
   */
  onOpenFile: (entry: ObjectEntry) => void;
  /**
   * Called on right-click with viewport coordinates. Rendering the actual
   * `ObjectContextMenu` is Block I — this is a plain pass-through callback
   * for now.
   */
  onContextMenu: (entry: ObjectEntry, x: number, y: number) => void;
}

function formatModified(lastModified: string): string {
  if (!lastModified) return '';
  const date = new Date(lastModified);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleDateString('ru-RU', { year: 'numeric', month: 'short', day: 'numeric' });
}

function truncateType(contentType: string): string {
  if (!contentType) return '—';
  return contentType.length > 20 ? `${contentType.slice(0, 20)}…` : contentType;
}

/**
 * Single row of the table view, per docs/03-ux-ui-spec.md section 5.4.3.
 * No per-row checkbox/actions cell — Stage 2 constraint (bulk select is
 * Stage 4; the only per-row actions come from the ПКМ context menu, Block I).
 */
export function FileRow({ entry, currentPrefix, onNavigateToFolder, onOpenFile, onContextMenu }: FileRowProps) {
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
      role="row"
      onDoubleClick={handleDoubleClick}
      onContextMenu={handleContextMenu}
      className="flex h-row shrink-0 items-center border-b border-border-subtle px-4 text-[13px] transition-colors duration-fast hover:bg-bg-tertiary"
    >
      <div className="flex min-w-0 flex-[3] items-center gap-2 pr-2">
        <FileIcon isFolder={entry.isFolder} contentType={entry.contentType} />
        <span className={cn('truncate text-fg-primary', entry.isFolder && 'font-semibold')} title={name}>
          {name}
          {entry.isFolder && '/'}
        </span>
      </div>
      <div className="flex-1 truncate pr-2 text-right font-mono text-xs text-fg-secondary">
        {entry.isFolder ? '—' : formatBytes(entry.size)}
      </div>
      <div className="flex-1 truncate pr-2 text-xs text-fg-muted" title={entry.contentType || undefined}>
        {entry.isFolder ? 'Папка' : truncateType(entry.contentType)}
      </div>
      <div className="flex-1 truncate text-xs text-fg-secondary">
        {entry.isFolder ? '' : formatModified(entry.lastModified)}
      </div>
    </div>
  );
}
