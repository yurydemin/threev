import type { TransferTask } from '../../types';

/**
 * The subset of `TransferTask`/`TransferHistoryEntry` needed to derive a
 * display name and source/destination line — both types share this shape,
 * so `TransferCard`/`HistoryCard` can use the same two helpers below.
 */
export interface TransferLike {
  type: string; // "upload" | "download"
  sourcePath: string;
  destinationPath: string;
}

export function isUpload(task: TransferLike): boolean {
  return task.type === 'upload';
}

/**
 * Last path segment of the S3-side field (`destinationPath` for uploads,
 * `sourcePath` for downloads — both encoded as `"bucket/key"`), used as the
 * card's file name.
 */
export function getTransferDisplayName(task: TransferLike): string {
  const s3Path = isUpload(task) ? task.destinationPath : task.sourcePath;
  const segments = s3Path.split('/').filter(Boolean);
  return segments[segments.length - 1] ?? s3Path;
}

/**
 * `"local path → s3://bucket/key"` for uploads, `"s3://bucket/key → local
 * path"` for downloads, per docs/03-ux-ui-spec.md section 5.5 middle row.
 */
export function getTransferPathLine(task: TransferLike): string {
  return isUpload(task)
    ? `${task.sourcePath} → s3://${task.destinationPath}`
    : `s3://${task.sourcePath} → ${task.destinationPath}`;
}

/** Narrows `TransferTask['type']`/`TransferHistoryEntry['type']` to a label + Tailwind color class. */
export function getTypeTagClasses(task: TransferLike): string {
  return isUpload(task) ? 'text-accent' : 'text-success';
}

export function getTypeLabel(task: TransferLike): string {
  return isUpload(task) ? 'Upload' : 'Download';
}

/** `Math.round` percentage, guarding against `totalBytes === 0` (division by zero → 0%). */
export function getProgressPercent(task: Pick<TransferTask, 'transferredBytes' | 'totalBytes'>): number {
  if (!task.totalBytes) return 0;
  return Math.round((task.transferredBytes / task.totalBytes) * 100);
}
