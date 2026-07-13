/**
 * Typed wrapper around the generated `wailsjs/go/filemanager/FileManagerService`
 * bindings.
 *
 * Responsibilities:
 * - Convert between the frontend-domain types (`types/index.ts`, camelCase)
 *   and the wailsjs-generated Go DTO classes (`domain.*`, PascalCase).
 * - Normalize rejected promises (Wails surfaces Go errors as string
 *   rejections) into a single `ApiError` shape the rest of the app can rely
 *   on (see `./errors`).
 *
 * Do not import `wailsjs/go/**` anywhere else in the app — go through this
 * module instead.
 */
import {
  CancelBulkOperation,
  CopyObjects,
  CreateFolder,
  DeleteObjects,
  GetPresignedURL,
  GetTextPreview,
  HeadObject,
  ListAllKeysUnderPrefix,
  ListBuckets,
  ListObjects,
  MoveObjects,
  RenameObject,
  UpdateMetadata,
} from '../../../wailsjs/go/filemanager/FileManagerService';
import { domain } from '../../../wailsjs/go/models';
import type {
  Bucket,
  ListObjectsRequest,
  ListObjectsResponse,
  ObjectEntry,
  ObjectMeta,
  TextPreviewResult,
} from '../../types';
import { call, toIsoString } from './errors';

function fromBucket(bucket: domain.Bucket): Bucket {
  return {
    name: bucket.Name,
    creationDate: toIsoString(bucket.CreationDate),
  };
}

function fromObjectEntry(entry: domain.ObjectEntry): ObjectEntry {
  return {
    key: entry.Key,
    isFolder: entry.IsFolder,
    size: entry.Size,
    contentType: entry.ContentType,
    lastModified: toIsoString(entry.LastModified),
    storageClass: entry.StorageClass,
  };
}

function fromListObjectsResponse(response: domain.ListObjectsResponse): ListObjectsResponse {
  return {
    entries: response.Entries.map(fromObjectEntry),
    nextContinuationToken: response.NextContinuationToken,
    isTruncated: response.IsTruncated,
  };
}

function toListObjectsRequest(request: ListObjectsRequest): domain.ListObjectsRequest {
  return domain.ListObjectsRequest.createFrom({
    ProfileID: request.profileId,
    Bucket: request.bucket,
    Prefix: request.prefix,
    ContinuationToken: request.continuationToken,
    SortBy: request.sortBy,
    SortOrder: request.sortOrder,
    Refresh: request.refresh,
  });
}

function fromObjectMeta(meta: domain.ObjectMeta): ObjectMeta {
  return {
    key: meta.Key,
    size: meta.Size,
    contentType: meta.ContentType,
    etag: meta.ETag,
    lastModified: toIsoString(meta.LastModified),
    metadata: meta.Metadata ?? {},
  };
}

function fromTextPreviewResult(result: domain.TextPreviewResult): TextPreviewResult {
  return {
    content: result.Content,
    truncated: result.Truncated,
    totalSize: result.TotalSize,
  };
}

export async function listBuckets(profileId: number): Promise<Bucket[]> {
  return call(async () => (await ListBuckets(profileId)).map(fromBucket));
}

/** Recursively lists every real object key under a folder's prefix (S3 has no native delete-by-prefix — this discovers what "delete folder" must actually remove). */
export async function listAllKeysUnderPrefix(profileId: number, bucket: string, prefix: string): Promise<string[]> {
  return call(() => ListAllKeysUnderPrefix(profileId, bucket, prefix));
}

export async function listObjects(request: ListObjectsRequest): Promise<ListObjectsResponse> {
  return call(async () => fromListObjectsResponse(await ListObjects(toListObjectsRequest(request))));
}

export async function headObject(profileId: number, bucket: string, key: string): Promise<ObjectMeta> {
  return call(async () => fromObjectMeta(await HeadObject(profileId, bucket, key)));
}

export async function getPresignedUrl(
  profileId: number,
  bucket: string,
  key: string,
  expirySeconds: number,
): Promise<string> {
  return call(() => GetPresignedURL(profileId, bucket, key, expirySeconds));
}

export async function getTextPreview(
  profileId: number,
  bucket: string,
  key: string,
): Promise<TextPreviewResult> {
  return call(async () => fromTextPreviewResult(await GetTextPreview(profileId, bucket, key)));
}

/** Deletes `keys` (async, bulk) — returns the new operation's id (`domain.BulkOperationProgressEvent.operationId`). */
export async function deleteObjects(profileId: number, bucket: string, keys: string[]): Promise<number> {
  return call(() =>
    DeleteObjects(domain.DeleteObjectsRequest.createFrom({ ProfileID: profileId, Bucket: bucket, Keys: keys })),
  );
}

/**
 * Copies `keys` (async, bulk) into `destBucket`/`destPrefix`, each keeping
 * its own basename (`destPrefix + basename(key)` — see
 * `internal/filemanager/copymove.go`, no destination renaming). Returns the
 * new operation's id.
 */
export async function copyObjects(
  profileId: number,
  sourceBucket: string,
  keys: string[],
  destBucket: string,
  destPrefix: string,
): Promise<number> {
  return call(() =>
    CopyObjects(
      domain.BulkCopyRequest.createFrom({
        ProfileID: profileId,
        SourceBucket: sourceBucket,
        Keys: keys,
        DestBucket: destBucket,
        DestPrefix: destPrefix,
      }),
    ),
  );
}

/** Same contract as `copyObjects`, but removes each source key after a successful copy. */
export async function moveObjects(
  profileId: number,
  sourceBucket: string,
  keys: string[],
  destBucket: string,
  destPrefix: string,
): Promise<number> {
  return call(() =>
    MoveObjects(
      domain.BulkMoveRequest.createFrom({
        ProfileID: profileId,
        SourceBucket: sourceBucket,
        Keys: keys,
        DestBucket: destBucket,
        DestPrefix: destPrefix,
      }),
    ),
  );
}

/** Cancels the in-flight bulk operation identified by `operationId` (see `useBulkOperationStore`). */
export async function cancelBulkOperation(operationId: number): Promise<void> {
  return call(() => CancelBulkOperation(operationId));
}

/**
 * Overwrites `key`'s `Content-Type`/`Cache-Control` headers and user
 * metadata (`userMetadata` has no `x-amz-meta-` prefix — see
 * `domain.ObjectMeta.Metadata`), synchronously (no operation id / progress
 * event — see `internal/filemanager/metadata.go`).
 */
export async function updateMetadata(
  profileId: number,
  bucket: string,
  key: string,
  contentType: string,
  cacheControl: string,
  userMetadata: Record<string, string>,
): Promise<void> {
  return call(() =>
    UpdateMetadata(
      domain.UpdateMetadataRequest.createFrom({
        ProfileID: profileId,
        Bucket: bucket,
        Key: key,
        ContentType: contentType,
        CacheControl: cacheControl,
        UserMetadata: userMetadata,
      }),
    ),
  );
}

/** Creates a zero-byte "folder marker" object at `prefix + name + "/"` (synchronous, no operation id). */
export async function createFolder(
  profileId: number,
  bucket: string,
  prefix: string,
  name: string,
): Promise<void> {
  return call(() =>
    CreateFolder(domain.CreateFolderRequest.createFrom({ ProfileID: profileId, Bucket: bucket, Prefix: prefix, Name: name })),
  );
}

/**
 * Renames a single object in place (same folder, new basename — server-side
 * copy + delete of the old key, synchronous, no operation id). NOT the same
 * as `moveObjects`: `BulkMoveRequest`/`BulkCopyRequest` always derive the
 * destination key as `destPrefix + basename(sourceKey)`, so they can change
 * an object's *location* but can never change its basename — only
 * `RenameObject` can do that.
 */
export async function renameObject(
  profileId: number,
  bucket: string,
  oldKey: string,
  newKey: string,
): Promise<void> {
  return call(() =>
    RenameObject(domain.RenameObjectRequest.createFrom({ ProfileID: profileId, Bucket: bucket, OldKey: oldKey, NewKey: newKey })),
  );
}
