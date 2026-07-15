/**
 * Typed wrapper around the generated `wailsjs/go/transfer/TransferService`
 * bindings.
 *
 * Responsibilities:
 * - Convert between the frontend-domain types (`types/index.ts`, camelCase)
 *   and the wailsjs-generated Go DTO classes (`domain.*`, PascalCase).
 * - Normalize rejected promises (Wails surfaces Go errors as string
 *   rejections) into a single `ApiError` shape the rest of the app can rely
 *   on (see `./errors`).
 *
 * `transfer:progress`/`object:change` event payloads are NOT handled here:
 * they never go through a generated binding (`wails generate module` only
 * scans bound service method signatures, not `runtime.EventsEmit` call
 * sites), so there is no `domain.*` class to convert from. That mapping
 * lives in `hooks/useTransferEvents.ts` instead.
 *
 * Do not import `wailsjs/go/**` anywhere else in the app — go through this
 * module instead.
 */
import {
  CancelTask,
  CancelTasksForProfile,
  ClearHistory,
  GetHistory,
  GetQueue,
  PauseTask,
  PickDownloadDestination,
  PickDownloadDirectory,
  PickUploadDirectory,
  PickUploadFiles,
  QueueDownload,
  QueueDownloadPrefix,
  QueueUpload,
  QueueUploadPaths,
  ReorderTask,
  ResumeTask,
  RetryTask,
} from '../../../wailsjs/go/transfer/TransferService';
import { domain } from '../../../wailsjs/go/models';
import type { DownloadRequest, TransferHistoryEntry, TransferTask, UploadRequest } from '../../types';
import { call, toIsoString } from './errors';

function fromTransferTask(task: domain.TransferTask): TransferTask {
  return {
    id: task.ID,
    profileId: task.ProfileID,
    type: task.Type,
    sourcePath: task.SourcePath,
    destinationPath: task.DestinationPath,
    status: task.Status,
    totalBytes: task.TotalBytes,
    transferredBytes: task.TransferredBytes,
    errorMessage: task.ErrorMessage,
    multipartUploadId: task.MultipartUploadID,
    priority: task.Priority,
    createdAt: toIsoString(task.CreatedAt),
    updatedAt: toIsoString(task.UpdatedAt),
  };
}

function fromTransferHistoryEntry(entry: domain.TransferHistoryEntry): TransferHistoryEntry {
  return {
    id: entry.ID,
    queueId: entry.QueueID,
    profileId: entry.ProfileID,
    type: entry.Type,
    sourcePath: entry.SourcePath,
    destinationPath: entry.DestinationPath,
    totalBytes: entry.TotalBytes,
    status: entry.Status,
    completedAt: toIsoString(entry.CompletedAt),
    errorMessage: entry.ErrorMessage,
  };
}

function toUploadRequest(request: UploadRequest): domain.UploadRequest {
  return domain.UploadRequest.createFrom({
    ProfileID: request.profileId,
    Bucket: request.bucket,
    Key: request.key,
    LocalPath: request.localPath,
    Priority: request.priority,
  });
}

function toDownloadRequest(request: DownloadRequest): domain.DownloadRequest {
  return domain.DownloadRequest.createFrom({
    ProfileID: request.profileId,
    Bucket: request.bucket,
    Key: request.key,
    LocalPath: request.localPath,
    Priority: request.priority,
  });
}

export async function queueUpload(request: UploadRequest): Promise<number> {
  return call(() => QueueUpload(toUploadRequest(request)));
}

export async function queueDownload(request: DownloadRequest): Promise<number> {
  return call(() => QueueDownload(toDownloadRequest(request)));
}

export async function queueUploadPaths(
  profileId: number,
  bucket: string,
  destinationPrefix: string,
  localPaths: string[],
): Promise<number[]> {
  return call(() => QueueUploadPaths(profileId, bucket, destinationPrefix, localPaths));
}

export async function queueDownloadPrefix(
  profileId: number,
  bucket: string,
  prefix: string,
  localDestDir: string,
): Promise<number[]> {
  return call(() => QueueDownloadPrefix(profileId, bucket, prefix, localDestDir));
}

export async function pauseTask(id: number): Promise<void> {
  return call(() => PauseTask(id));
}

export async function resumeTask(id: number): Promise<void> {
  return call(() => ResumeTask(id));
}

export async function cancelTask(id: number): Promise<void> {
  return call(() => CancelTask(id));
}

export async function cancelTasksForProfile(profileId: number): Promise<number> {
  return call(() => CancelTasksForProfile(profileId));
}

export async function retryTask(id: number): Promise<number> {
  return call(() => RetryTask(id));
}

export async function reorderTask(id: number, newPriority: number): Promise<void> {
  return call(() => ReorderTask(id, newPriority));
}

export async function getQueue(): Promise<TransferTask[]> {
  return call(async () => (await GetQueue()).map(fromTransferTask));
}

export async function getHistory(limit: number): Promise<TransferHistoryEntry[]> {
  return call(async () => (await GetHistory(limit)).map(fromTransferHistoryEntry));
}

export async function clearHistory(): Promise<void> {
  return call(() => ClearHistory());
}

export async function pickUploadFiles(): Promise<string[]> {
  return call(() => PickUploadFiles());
}

export async function pickUploadDirectory(): Promise<string> {
  return call(() => PickUploadDirectory());
}

export async function pickDownloadDestination(defaultFilename: string): Promise<string> {
  return call(() => PickDownloadDestination(defaultFilename));
}

export async function pickDownloadDirectory(): Promise<string> {
  return call(() => PickDownloadDirectory());
}
