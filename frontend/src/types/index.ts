/**
 * Frontend-domain types mirroring Go DTOs exposed by `ConnectionService`
 * and `FileManagerService` (see `wailsjs/go/models.ts`, namespace `domain`).
 *
 * These are plain TS interfaces rather than re-exports of the wailsjs
 * classes: the generated classes carry extra runtime machinery
 * (`convertValues`, `createFrom`) that UI code has no business depending on.
 *
 * Naming follows the frontend domain convention ("connection"), while the
 * Go backend keeps its own naming ("Profile"/"ProfileDTO") — see
 * constraint #12 in the Stage 1 plan.
 */

/** Full connection record, mirrors `domain.Profile` (includes secrets). */
export interface Connection {
  id: number;
  name: string;
  endpointUrl: string;
  region: string;
  accessKeyId: string;
  secretAccessKey: string;
  sessionToken: string;
  pathStyle: boolean;
  verifySsl: boolean;
  customHeaders: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

/** Lightweight connection summary, mirrors `domain.ProfileDTO` (no secrets). */
export interface ConnectionSummary {
  id: number;
  name: string;
  endpointUrl: string;
  region: string;
  pathStyle: boolean;
  verifySsl: boolean;
  createdAt: string;
  updatedAt: string;
}

/** Mirrors `domain.ConnectionTestResult`. */
export interface ConnectionTestResult {
  success: boolean;
  message: string;
  detail: string;
  category: string;
}

/**
 * The subset of `Connection` fields actually edited in the connection
 * form. `id`/`createdAt`/`updatedAt` are managed separately (assigned by
 * the backend / the store), not by form state.
 */
export type ConnectionFormValues = Omit<Connection, 'id' | 'createdAt' | 'updatedAt'>;

/**
 * Bookmarked bucket/prefix location, mirrors `domain.Favorite`. Uniquely
 * identified by (profileId, bucket, prefix) on the backend; deliberately has
 * no label/name field — display text (`bucket`, or `bucket/prefix`) is
 * always computed by the frontend from `bucket`/`prefix`, never stored.
 */
export interface Favorite {
  id: number;
  profileId: number;
  /** Owning profile's display name, joined server-side — used to group the Sidebar's favorites list. */
  profileName: string;
  bucket: string;
  /** Empty string = bucket root, never null. */
  prefix: string;
  createdAt: string;
}

/** Mirrors `domain.Bucket`. */
export interface Bucket {
  name: string;
  creationDate: string;
}

/** Mirrors `domain.ObjectEntry`. */
export interface ObjectEntry {
  key: string;
  isFolder: boolean;
  size: number;
  contentType: string;
  lastModified: string;
  storageClass: string;
}

/** Mirrors `domain.ListObjectsRequest`. */
export interface ListObjectsRequest {
  profileId: number;
  bucket: string;
  prefix: string;
  continuationToken: string;
  sortBy: string;
  sortOrder: string;
  refresh: boolean;
}

/** Mirrors `domain.ListObjectsResponse`. */
export interface ListObjectsResponse {
  entries: ObjectEntry[];
  nextContinuationToken: string;
  isTruncated: boolean;
}

/** Mirrors `domain.ObjectMeta`. */
export interface ObjectMeta {
  key: string;
  size: number;
  contentType: string;
  etag: string;
  lastModified: string;
  metadata: Record<string, string>;
}

/** Mirrors `domain.TextPreviewResult`. */
export interface TextPreviewResult {
  content: string;
  truncated: boolean;
  totalSize: number;
}

/** Mirrors `domain.BucketSizeResult` (see `filemanager/bucketsize.go`'s recursive walk, same shape as `ListAllKeysUnderPrefix` but summing size/count instead of collecting keys). */
export interface BucketSizeResult {
  totalBytes: number;
  objectCount: number;
  /** `true` if the recursive walk hit its internal timeout before finishing — the totals below are a partial count, not the real bucket size. */
  truncated: boolean;
}

/** Mirrors `domain.TransferTask`. Status is one of "pending" | "running" | "paused" | "completed" | "failed" | "cancelled" (FR-QUEUE-002). */
export interface TransferTask {
  id: number;
  profileId: number;
  type: string; // "upload" | "download"
  sourcePath: string;
  destinationPath: string;
  status: string;
  totalBytes: number;
  transferredBytes: number;
  errorMessage: string;
  multipartUploadId: string;
  priority: number;
  createdAt: string;
  updatedAt: string;
}

/** Mirrors `domain.TransferHistoryEntry`. */
export interface TransferHistoryEntry {
  id: number;
  queueId: number;
  profileId: number;
  type: string;
  sourcePath: string;
  destinationPath: string;
  totalBytes: number;
  status: string;
  completedAt: string;
  errorMessage: string;
}

/** Input to `TransferService.QueueUpload`, mirrors `domain.UploadRequest`. */
export interface UploadRequest {
  profileId: number;
  bucket: string;
  key: string;
  localPath: string;
  priority: number;
}

/** Input to `TransferService.QueueDownload`, mirrors `domain.DownloadRequest`. */
export interface DownloadRequest {
  profileId: number;
  bucket: string;
  key: string;
  localPath: string;
  priority: number;
}

/**
 * Payload of the Wails "transfer:progress" event, mirrors
 * `domain.TransferProgressEvent`. NOT part of the generated wailsjs
 * bindings (`wails generate module` only scans bound service method
 * signatures, not `runtime.EventsEmit` payloads) - received in
 * `hooks/useTransferEvents.ts` as a raw PascalCase object and mapped
 * manually.
 */
export interface TransferProgressEvent {
  taskId: number;
  transferredBytes: number;
  totalBytes: number;
  speedBytesPerSec: number;
  etaSeconds: number;
  status: string;
  error: string;
}

/**
 * Payload of the Wails "object:change" event, mirrors
 * `domain.ObjectChangeEvent`. Same caveat as `TransferProgressEvent` - not
 * part of the generated bindings.
 */
export interface ObjectChangeEvent {
  bucket: string;
  prefix: string;
  type: string; // "create" | "delete"
}

/**
 * Payload of the Wails "bulk:progress" event, mirrors
 * `domain.BulkOperationProgressEvent`. NOT part of the generated wailsjs
 * bindings (`wails generate module` only scans bound service method
 * signatures, not `runtime.EventsEmit` payloads) - received in
 * `hooks/useBulkOperationEvents.ts` as a raw PascalCase object and mapped
 * manually.
 */
export interface BulkOperationProgressEvent {
  operationId: number;
  type: string; // "delete" | "copy" | "move"
  total: number;
  completed: number;
  failedCount: number;
  status: string; // "running" | "completed" | "cancelled"
}

/** Mirrors `domain.AppSettings`. */
export interface AppSettings {
  theme: string; // "system" | "light" | "dark"
  uiScalePercent: number; // 90-125
  closeBehavior: string; // "exit" | "confirm"
  autoResumeQueue: boolean;
  maxConcurrentTransfers: number; // 1-10
  partSizeOverrideMB: number; // 0 = adaptive, otherwise 5-128
  bandwidthLimitUploadBytesPerSec: number;
  bandwidthLimitDownloadBytesPerSec: number;
}
