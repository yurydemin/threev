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
  GetPresignedURL,
  GetTextPreview,
  HeadObject,
  ListBuckets,
  ListObjects,
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
