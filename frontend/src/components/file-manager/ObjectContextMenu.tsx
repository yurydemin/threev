import { Copy, Eye, FolderOpen, Link } from 'lucide-react';
import { ContextMenu, type ContextMenuItem } from '../ui/ContextMenu';
import { getPresignedUrl } from '../../lib/wails/fileManager';
import { getPreviewKind } from '../../lib/preview';
import { getEntryDisplayName } from '../../lib/utils';
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
}

async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch (err) {
    // No toast system yet (Stage 4, docs/03-ux-ui-spec.md section 4.8) — a
    // failed clipboard write (e.g. denied permission) fails silently rather
    // than crashing the menu interaction.
    console.error('[ObjectContextMenu] clipboard write failed:', err);
  }
}

/**
 * ПКМ context menu for a single object, per docs/03-ux-ui-spec.md section
 * 5.4.5 — trimmed to the Stage 2 constraint 6 subset: "Открыть /
 * Предпросмотр" (only if the type is previewable), "Копировать URL",
 * "Скопировать имя", "Скопировать путь". "Скачать...", "Изменить
 * метаданные...", "Удалить" are not shown at all (those services don't
 * exist yet — Stage 3/4).
 *
 * Reads `activeProfileId`/`selectedBucket`/`currentPrefix` directly from
 * `useFileManagerStore`, same convention as `Toolbar`/`BucketPanel`/
 * `FileList`, so the only props are the ones that vary per invocation
 * (which entry, where to render, how to open a preview).
 *
 * For folders, the menu is reduced to a single "Открыть" item (navigates via
 * the store directly — no separate `onNavigate` prop) for consistency with
 * common file managers; double-click already covers the same action, but a
 * folder right-click with an empty menu would look broken. None of the
 * file-only items (copy URL/name/path) apply: folders aren't S3 objects
 * with their own presigned URL, and "copy name/path" wasn't asked for on
 * folders in the reduced constraint-6 set.
 *
 * The underlying `ContextMenu` primitive already calls `onClose()`
 * synchronously before `item.onClick()` (see `components/ui/ContextMenu.tsx`),
 * so the menu is gone before an async action like "Копировать URL" even
 * resolves — there is no window to swap the clicked item's label to a
 * "Скопировано!" confirmation. Absent a toast system (Stage 4), closing the
 * menu immediately *is* the feedback; clipboard failures are logged, not
 * surfaced, per the same reasoning as above.
 */
export function ObjectContextMenu({ entry, x, y, onClose, onOpenPreview }: ObjectContextMenuProps) {
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const navigateToPrefix = useFileManagerStore((state) => state.navigateToPrefix);

  if (!entry) return null;

  if (entry.isFolder) {
    const items: ContextMenuItem[] = [
      {
        label: 'Открыть',
        icon: <FolderOpen className="h-4 w-4" aria-hidden="true" />,
        onClick: () => navigateToPrefix(entry.key),
      },
    ];
    return <ContextMenu x={x} y={y} items={items} onClose={onClose} />;
  }

  const previewKind = getPreviewKind(entry.contentType);
  const displayName = getEntryDisplayName(entry.key, currentPrefix);

  const items: ContextMenuItem[] = [];

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
        .catch((err) => console.error('[ObjectContextMenu] getPresignedUrl failed:', err));
    },
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

  return <ContextMenu x={x} y={y} items={items} onClose={onClose} />;
}
