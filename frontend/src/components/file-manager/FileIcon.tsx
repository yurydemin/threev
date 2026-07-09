import { File, FileArchive, FileImage, FileText, Folder, type LucideIcon } from 'lucide-react';
import { cn } from '../../lib/utils';

export interface FileIconProps {
  isFolder: boolean;
  /** MIME type as returned by `ListObjects` (may be empty for folders). */
  contentType: string;
  /** Icon size in pixels. Defaults to `ICON_SIZE` (16px, docs/03-ux-ui-spec.md section 3). */
  size?: number;
  className?: string;
}

const ARCHIVE_CONTENT_TYPES = new Set([
  'application/zip',
  'application/x-zip-compressed',
  'application/x-tar',
  'application/gzip',
  'application/x-gzip',
  'application/x-7z-compressed',
  'application/x-rar-compressed',
  'application/x-bzip2',
]);

// Non-`text/*` MIME types that still read as "text/code" content, per
// docs/03-ux-ui-spec.md section 8 (`FileText`/`FileCode` both map to the
// same `FileText` icon here — lucide has no dedicated PDF glyph, and
// `FileText` reads fine for it too, per the Block H task notes).
const TEXT_LIKE_CONTENT_TYPES = new Set([
  'application/json',
  'application/xml',
  'application/javascript',
  'application/x-javascript',
  'application/x-yaml',
  'application/yaml',
  'application/pdf',
]);

interface IconResolution {
  Icon: LucideIcon;
  colorClass: string;
}

function resolveIcon(isFolder: boolean, contentType: string): IconResolution {
  if (isFolder) return { Icon: Folder, colorClass: 'text-accent' };
  if (contentType.startsWith('image/')) return { Icon: FileImage, colorClass: 'text-warning' };
  if (ARCHIVE_CONTENT_TYPES.has(contentType)) return { Icon: FileArchive, colorClass: 'text-fg-secondary' };
  if (contentType.startsWith('text/') || TEXT_LIKE_CONTENT_TYPES.has(contentType)) {
    return { Icon: FileText, colorClass: 'text-fg-secondary' };
  }
  return { Icon: File, colorClass: 'text-fg-secondary' };
}

/**
 * File/folder type icon, per docs/03-ux-ui-spec.md sections 5.4.3 ("иконка
 * типа: папка `--accent`, изображение `--warning`, документ
 * `--fg-secondary`") and 8 (icon-per-type table).
 *
 * Takes `isFolder`/`contentType` directly rather than a full `ObjectEntry`
 * so it composes in both `FileRow` and `FileGridItem` without those needing
 * to reach into a shared entry shape just for two fields.
 */
export function FileIcon({ isFolder, contentType, size = 16, className }: FileIconProps) {
  const { Icon, colorClass } = resolveIcon(isFolder, contentType);
  return <Icon size={size} className={cn('shrink-0', colorClass, className)} aria-hidden="true" />;
}
