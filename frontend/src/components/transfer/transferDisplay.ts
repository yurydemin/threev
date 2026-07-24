import type { TransferTask } from '../../types';

/**
 * The subset of `TransferTask`/`TransferHistoryEntry` needed to derive a
 * display name and source/destination line — both types share this shape,
 * so `TransferCard`/`HistoryCard` can use the same two helpers below.
 *
 * `isMove` is optional: it only exists on `TransferTask` (a "copy_cross"
 * task's copy-vs-move flag, see `domain.TransferTask.IsMove`'s doc
 * comment) — `TransferHistoryEntry` doesn't carry it (an archived
 * "copy_cross" entry can no longer be labeled "Move" vs "Copy", an
 * accepted MVP limitation of `archiveTask`, internal/transfer/service.go),
 * so `getTypeLabel` falls back to the generic "Copy" label whenever it's
 * absent.
 */
export interface TransferLike {
  type: string; // "upload" | "download" | "download_zip" | "copy_cross"
  sourcePath: string;
  destinationPath: string;
  isMove?: boolean;
}

export function isUpload(task: TransferLike): boolean {
  return task.type === 'upload';
}

function isCrossConnectionCopy(task: TransferLike): boolean {
  return task.type === 'copy_cross';
}

/**
 * Last path segment of the S3-side field (`destinationPath` for uploads,
 * `sourcePath` for downloads/copy_cross — both encoded as `"bucket/key"`),
 * used as the card's file name.
 */
export function getTransferDisplayName(task: TransferLike): string {
  const s3Path = isUpload(task) ? task.destinationPath : task.sourcePath;
  const segments = s3Path.split('/').filter(Boolean);
  return segments[segments.length - 1] ?? s3Path;
}

/**
 * `"local path → s3://bucket/key"` for uploads, `"s3://bucket/key → local
 * path"` for downloads, `"s3://bucket/key → s3://bucket/key"` for
 * copy_cross (both `sourcePath`/`destinationPath` are S3 bucket/key pairs
 * there too — resolved against two different connection profiles, see
 * `domain.TransferTask.DestinationPath`'s doc comment — so, unlike a plain
 * download, the destination side needs the `s3://` prefix too), per
 * docs/03-ux-ui-spec.md section 5.5 middle row.
 */
export function getTransferPathLine(task: TransferLike): string {
  if (isCrossConnectionCopy(task)) {
    return `s3://${task.sourcePath} → s3://${task.destinationPath}`;
  }
  return isUpload(task)
    ? `${task.sourcePath} → s3://${task.destinationPath}`
    : `s3://${task.sourcePath} → ${task.destinationPath}`;
}

/** Narrows `TransferTask['type']`/`TransferHistoryEntry['type']` to a label + Tailwind color class. */
export function getTypeTagClasses(task: TransferLike): string {
  if (isCrossConnectionCopy(task)) return 'text-warning';
  return isUpload(task) ? 'text-accent' : 'text-success';
}

export function getTypeLabel(task: TransferLike): string {
  if (isCrossConnectionCopy(task)) return task.isMove ? 'Move' : 'Copy';
  return isUpload(task) ? 'Upload' : 'Download';
}

/** `Math.round` percentage, guarding against `totalBytes === 0` (division by zero → 0%). */
export function getProgressPercent(task: Pick<TransferTask, 'transferredBytes' | 'totalBytes'>): number {
  if (!task.totalBytes) return 0;
  return Math.round((task.transferredBytes / task.totalBytes) * 100);
}
