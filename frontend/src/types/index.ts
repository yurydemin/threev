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
