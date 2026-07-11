import {
  Copy,
  CopyPlus,
  Download,
  Eye,
  FolderInput,
  FolderOpen,
  Link,
  Link2,
  Pencil,
  Settings2,
  Trash2,
} from 'lucide-react';
import { ContextMenu, type ContextMenuItem } from '../ui/ContextMenu';
import { getPresignedUrl } from '../../lib/wails/fileManager';
import { pickDownloadDestination, pickDownloadDirectory } from '../../lib/wails/transfer';
import { useTransferStore } from '../../stores/useTransferStore';
import { getPreviewKind } from '../../lib/preview';
import { copyToClipboard, getEntryDisplayName } from '../../lib/utils';
import { toast } from '../../lib/toast';
import { ApiError } from '../../lib/wails/errors';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import type { ObjectEntry } from '../../types';

/** `getPresignedUrl`'s TTL for "Копировать URL" — Stage 2 constraint 2 (fixed 5 min). */
const COPY_URL_EXPIRY_SECONDS = 300;

export interface ObjectContextMenuProps {
  entry: ObjectEntry | null;
  x: number;
  y: number;
  onClose: () => void;
  /** Called for files whose type supports preview (see `lib/preview.ts`). */
  onOpenPreview: (entry: ObjectEntry) => void;
  /** Bulk or single delete — `keys` is `[entry.key]` outside a multi-selection context. */
  onDelete: (keys: string[]) => void;
  /** Bulk or single copy — opens `DestinationPickerModal` mode="copy". */
  onCopy: (keys: string[]) => void;
  /** Bulk or single move — opens `DestinationPickerModal` mode="move". */
  onMove: (keys: string[]) => void;
  /** Single-object rename (never offered in the bulk branch — rename only makes sense for one object at a time). */
  onRename: (entry: ObjectEntry) => void;
  /** Opens `PropertiesModal` for a single object. */
  onEditMetadata: (entry: ObjectEntry) => void;
  /** Opens `PresignedUrlModal` for a single object. */
  onGetPresignedUrl: (entry: ObjectEntry) => void;
}

/**
 * ПКМ context menu for a single object, per docs/03-ux-ui-spec.md section
 * 5.4.5, extended in Stage 4 Block D with bulk actions (delete/copy/move)
 * and the previously-missing single-object actions (copy/move/rename/edit
 * metadata/delete) now that their backing services exist.
 *
 * Reads `activeProfileId`/`selectedBucket`/`currentPrefix`/`selectedKeys`
 * directly from `useFileManagerStore`, same convention as `Toolbar`/
 * `BucketPanel`/`FileList`, so the only props are the ones that vary per
 * invocation (which entry, where to render, how to open a preview/modal).
 *
 * Modal-opening actions (delete/copy/move/rename/metadata/presigned URL) are
 * all routed through callback props rather than rendered from inside this
 * component — `ObjectContextMenu` does not own modal state, the same
 * established pattern `onOpenPreview` already follows (modals live in
 * `FileManagerScreen`).
 *
 * `isBulkContext` (right-clicking a file that's part of a ≥2-item
 * multi-selection) swaps in a reduced bulk-only item set operating on
 * `selectedKeys` rather than the single right-clicked `entry`. Folders are
 * never part of a multi-selection (`useFileManagerStore.toggleSelect` is a
 * no-op for folder entries), so the bulk branch only ever applies to files.
 */
export function ObjectContextMenu({
  entry,
  x,
  y,
  onClose,
  onOpenPreview,
  onDelete,
  onCopy,
  onMove,
  onRename,
  onEditMetadata,
  onGetPresignedUrl,
}: ObjectContextMenuProps) {
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const navigateToPrefix = useFileManagerStore((state) => state.navigateToPrefix);
  const selectedKeys = useFileManagerStore((state) => state.selectedKeys);

  if (!entry) return null;

  if (entry.isFolder) {
    const items: ContextMenuItem[] = [
      {
        label: 'Скачать',
        icon: <Download className="h-4 w-4" aria-hidden="true" />,
        disabled: !activeProfileId || !selectedBucket,
        onClick: () => {
          if (!activeProfileId || !selectedBucket) return;
          void pickDownloadDirectory()
            .then((dir) => {
              if (!dir) return;
              return useTransferStore
                .getState()
                .queueDownloadPrefix(activeProfileId, selectedBucket, entry.key, dir);
            })
            .catch((err) => {
              console.error('[ObjectContextMenu] pickDownloadDirectory failed:', err);
              toast.error(
                err instanceof ApiError ? err.message : 'Не удалось выбрать папку для скачивания',
                err instanceof ApiError ? err.raw : undefined,
              );
            });
        },
      },
      {
        label: 'Открыть',
        icon: <FolderOpen className="h-4 w-4" aria-hidden="true" />,
        onClick: () => navigateToPrefix(entry.key),
      },
    ];
    return <ContextMenu x={x} y={y} items={items} onClose={onClose} />;
  }

  const isBulkContext = selectedKeys.size > 1 && selectedKeys.has(entry.key);

  if (isBulkContext) {
    const keys = Array.from(selectedKeys);
    const items: ContextMenuItem[] = [
      {
        label: 'Скачать выбранные',
        icon: <Download className="h-4 w-4" aria-hidden="true" />,
        disabled: !activeProfileId || !selectedBucket,
        onClick: () => {
          if (!activeProfileId || !selectedBucket) return;
          void pickDownloadDirectory()
            .then((dir) => {
              if (!dir) return;
              // Each `queueDownload` swallows its own errors internally
              // (`useTransferStore.queueDownload` catches and returns `null`
              // rather than rejecting), so one failing key never stops the
              // rest of the loop from being queued.
              for (const key of keys) {
                void useTransferStore.getState().queueDownload({
                  profileId: activeProfileId,
                  bucket: selectedBucket,
                  key,
                  localPath: `${dir}/${key.split('/').pop()}`,
                  priority: 0,
                });
              }
            })
            .catch((err) => {
              console.error('[ObjectContextMenu] pickDownloadDirectory failed:', err);
              toast.error(
                err instanceof ApiError ? err.message : 'Не удалось выбрать папку для скачивания',
                err instanceof ApiError ? err.raw : undefined,
              );
            });
        },
      },
      {
        label: 'Копировать...',
        icon: <CopyPlus className="h-4 w-4" aria-hidden="true" />,
        onClick: () => onCopy(keys),
      },
      {
        label: 'Переместить...',
        icon: <FolderInput className="h-4 w-4" aria-hidden="true" />,
        onClick: () => onMove(keys),
      },
      { separator: true },
      {
        label: `Удалить ${keys.length} объектов`,
        icon: <Trash2 className="h-4 w-4" aria-hidden="true" />,
        destructive: true,
        onClick: () => onDelete(keys),
      },
    ];
    return <ContextMenu x={x} y={y} items={items} onClose={onClose} />;
  }

  const previewKind = getPreviewKind(entry.contentType);
  const displayName = getEntryDisplayName(entry.key, currentPrefix);

  const items: ContextMenuItem[] = [];

  items.push({
    label: 'Скачать...',
    icon: <Download className="h-4 w-4" aria-hidden="true" />,
    disabled: !activeProfileId || !selectedBucket,
    onClick: () => {
      if (!activeProfileId || !selectedBucket) return;
      void pickDownloadDestination(displayName)
        .then((localPath) => {
          if (!localPath) return;
          return useTransferStore.getState().queueDownload({
            profileId: activeProfileId,
            bucket: selectedBucket,
            key: entry.key,
            localPath,
            priority: 0,
          });
        })
        .catch((err) => {
          console.error('[ObjectContextMenu] pickDownloadDestination failed:', err);
          toast.error(
            err instanceof ApiError ? err.message : 'Не удалось выбрать место для скачивания',
            err instanceof ApiError ? err.raw : undefined,
          );
        });
    },
  });

  if (previewKind) {
    items.push({
      label: 'Открыть / Предпросмотр',
      icon: <Eye className="h-4 w-4" aria-hidden="true" />,
      onClick: () => onOpenPreview(entry),
    });
  }

  items.push({
    label: 'Копировать URL',
    icon: <Link className="h-4 w-4" aria-hidden="true" />,
    disabled: !activeProfileId || !selectedBucket,
    onClick: () => {
      if (!activeProfileId || !selectedBucket) return;
      void getPresignedUrl(activeProfileId, selectedBucket, entry.key, COPY_URL_EXPIRY_SECONDS)
        .then((url) => copyToClipboard(url))
        .catch((err) => {
          console.error('[ObjectContextMenu] getPresignedUrl failed:', err);
          toast.error(
            err instanceof ApiError ? err.message : 'Не удалось получить presigned URL',
            err instanceof ApiError ? err.raw : undefined,
          );
        });
    },
  });

  items.push({
    label: 'Получить presigned URL...',
    icon: <Link2 className="h-4 w-4" aria-hidden="true" />,
    onClick: () => onGetPresignedUrl(entry),
  });

  items.push({ separator: true });

  items.push({
    label: 'Копировать...',
    icon: <CopyPlus className="h-4 w-4" aria-hidden="true" />,
    onClick: () => onCopy([entry.key]),
  });

  items.push({
    label: 'Переместить...',
    icon: <FolderInput className="h-4 w-4" aria-hidden="true" />,
    onClick: () => onMove([entry.key]),
  });

  items.push({
    label: 'Переименовать',
    icon: <Pencil className="h-4 w-4" aria-hidden="true" />,
    onClick: () => onRename(entry),
  });

  items.push({ separator: true });

  items.push({
    label: 'Изменить метаданные...',
    icon: <Settings2 className="h-4 w-4" aria-hidden="true" />,
    onClick: () => onEditMetadata(entry),
  });

  items.push({ separator: true });

  items.push({
    label: 'Скопировать имя',
    icon: <Copy className="h-4 w-4" aria-hidden="true" />,
    onClick: () => void copyToClipboard(displayName),
  });

  items.push({
    label: 'Скопировать путь',
    icon: <Copy className="h-4 w-4" aria-hidden="true" />,
    disabled: !selectedBucket,
    onClick: () => {
      if (!selectedBucket) return;
      void copyToClipboard(`s3://${selectedBucket}/${entry.key}`);
    },
  });

  items.push({ separator: true });

  items.push({
    label: 'Удалить',
    icon: <Trash2 className="h-4 w-4" aria-hidden="true" />,
    destructive: true,
    onClick: () => onDelete([entry.key]),
  });

  return <ContextMenu x={x} y={y} items={items} onClose={onClose} />;
}
