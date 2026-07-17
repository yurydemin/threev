import { create } from 'zustand';
import {
  cancelTask,
  clearHistory as clearHistoryApi,
  getHistory,
  getQueue,
  pauseTask,
  queueDownload as queueDownloadApi,
  queueDownloadPrefix as queueDownloadPrefixApi,
  queueDownloadPrefixZip as queueDownloadPrefixZipApi,
  queueUpload as queueUploadApi,
  queueUploadPaths as queueUploadPathsApi,
  reorderTask,
  resumeTask,
  retryTask,
} from '../lib/wails/transfer';
import { ApiError } from '../lib/wails/errors';
import type { DownloadRequest, TransferHistoryEntry, TransferProgressEvent, TransferTask, UploadRequest } from '../types';

/** Task statuses that move a task from `queue` to `history` on the backend (Stage 3 Block F). */
const TERMINAL_STATUSES = new Set(['completed', 'cancelled']);

const DEFAULT_HISTORY_LIMIT = 100;

interface TransferState {
  queue: TransferTask[];
  history: TransferHistoryEntry[];
  isLoadingQueue: boolean;
  isLoadingHistory: boolean;
  queueError: string | null;
  historyError: string | null;
  /**
   * Latest `speedBytesPerSec`/`etaSeconds` seen per task id, keyed off
   * "transfer:progress" events (Stage 3 Block I). `TransferTask` itself
   * (from `GetQueue()`) carries neither field - they only exist on the
   * event payload - so these maps are the sole source for the Transfer
   * screen's speed/ETA display. Entries are dropped once a task leaves
   * `queue` (moves to `history` or is otherwise removed), so a stale speed
   * never lingers on a task id that gets reused.
   */
  speedByTaskId: Record<number, number>;
  etaByTaskId: Record<number, number>;

  fetchQueue: () => Promise<void>;
  fetchHistory: (limit?: number) => Promise<void>;

  queueUpload: (request: UploadRequest) => Promise<number | null>;
  queueDownload: (request: DownloadRequest) => Promise<number | null>;
  queueUploadPaths: (
    profileId: number,
    bucket: string,
    destinationPrefix: string,
    localPaths: string[],
  ) => Promise<number[]>;
  queueDownloadPrefix: (
    profileId: number,
    bucket: string,
    prefix: string,
    localDestDir: string,
  ) => Promise<number[]>;
  queueDownloadPrefixZip: (
    profileId: number,
    bucket: string,
    prefix: string,
    localZipPath: string,
  ) => Promise<number | null>;
  pauseTask: (id: number) => Promise<void>;
  resumeTask: (id: number) => Promise<void>;
  cancelTask: (id: number) => Promise<void>;
  retryTask: (id: number) => Promise<void>;
  reorderTask: (id: number, newPriority: number) => Promise<void>;
  clearHistory: () => Promise<void>;

  /**
   * Applies one "transfer:progress" event to `queue`/`history` in place -
   * called by `hooks/useTransferEvents.ts` on every event, never awaits a
   * network round-trip itself (see the function body for the "event before
   * fetchQueue" race and why it's safe to just drop the event in that case).
   */
  applyProgressEvent: (event: TransferProgressEvent) => void;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}

/**
 * Transfer queue/history state, backed by `TransferService` via
 * `lib/wails/transfer.ts`.
 *
 * Errors are captured into `queueError`/`historyError` rather than
 * re-thrown, following the same pattern as `useFileManagerStore`.
 *
 * Mutating actions (`queueUpload`, `pauseTask`, ...) re-fetch `queue` (or
 * `history` for `clearHistory`) after a successful call, rather than
 * optimistically patching local state: the backend is the source of truth
 * for fields the caller doesn't control (e.g. `status`, `createdAt`), and
 * the queue is small/cheap to refetch. This also closes the gap between a
 * task being created and its first "transfer:progress" event.
 */
export const useTransferStore = create<TransferState>()((set, get) => ({
  queue: [],
  history: [],
  isLoadingQueue: false,
  isLoadingHistory: false,
  queueError: null,
  historyError: null,
  speedByTaskId: {},
  etaByTaskId: {},

  fetchQueue: async () => {
    set({ isLoadingQueue: true, queueError: null });
    try {
      const queue = await getQueue();
      set({ queue, isLoadingQueue: false });
    } catch (err) {
      set({ queueError: errorMessage(err), isLoadingQueue: false });
    }
  },

  fetchHistory: async (limit = DEFAULT_HISTORY_LIMIT) => {
    set({ isLoadingHistory: true, historyError: null });
    try {
      const history = await getHistory(limit);
      set({ history, isLoadingHistory: false });
    } catch (err) {
      set({ historyError: errorMessage(err), isLoadingHistory: false });
    }
  },

  queueUpload: async (request) => {
    try {
      const id = await queueUploadApi(request);
      await get().fetchQueue();
      return id;
    } catch (err) {
      set({ queueError: errorMessage(err) });
      return null;
    }
  },

  queueDownload: async (request) => {
    try {
      const id = await queueDownloadApi(request);
      await get().fetchQueue();
      return id;
    } catch (err) {
      set({ queueError: errorMessage(err) });
      return null;
    }
  },

  queueUploadPaths: async (profileId, bucket, destinationPrefix, localPaths) => {
    try {
      const ids = await queueUploadPathsApi(profileId, bucket, destinationPrefix, localPaths);
      await get().fetchQueue();
      return ids;
    } catch (err) {
      set({ queueError: errorMessage(err) });
      return [];
    }
  },

  queueDownloadPrefix: async (profileId, bucket, prefix, localDestDir) => {
    try {
      const ids = await queueDownloadPrefixApi(profileId, bucket, prefix, localDestDir);
      await get().fetchQueue();
      return ids;
    } catch (err) {
      set({ queueError: errorMessage(err) });
      return [];
    }
  },

  queueDownloadPrefixZip: async (profileId, bucket, prefix, localZipPath) => {
    try {
      const id = await queueDownloadPrefixZipApi(profileId, bucket, prefix, localZipPath);
      await get().fetchQueue();
      return id;
    } catch (err) {
      set({ queueError: errorMessage(err) });
      return null;
    }
  },

  pauseTask: async (id) => {
    try {
      await pauseTask(id);
      await get().fetchQueue();
    } catch (err) {
      set({ queueError: errorMessage(err) });
    }
  },

  resumeTask: async (id) => {
    try {
      await resumeTask(id);
      await get().fetchQueue();
    } catch (err) {
      set({ queueError: errorMessage(err) });
    }
  },

  cancelTask: async (id) => {
    try {
      await cancelTask(id);
      await get().fetchQueue();
    } catch (err) {
      set({ queueError: errorMessage(err) });
    }
  },

  retryTask: async (id) => {
    try {
      await retryTask(id);
      await get().fetchQueue();
    } catch (err) {
      set({ queueError: errorMessage(err) });
    }
  },

  reorderTask: async (id, newPriority) => {
    try {
      await reorderTask(id, newPriority);
      await get().fetchQueue();
    } catch (err) {
      set({ queueError: errorMessage(err) });
    }
  },

  clearHistory: async () => {
    try {
      await clearHistoryApi();
      await get().fetchHistory();
    } catch (err) {
      set({ historyError: errorMessage(err) });
    }
  },

  applyProgressEvent: (event) => {
    if (TERMINAL_STATUSES.has(event.status)) {
      const speedByTaskId = { ...get().speedByTaskId };
      const etaByTaskId = { ...get().etaByTaskId };
      delete speedByTaskId[event.taskId];
      delete etaByTaskId[event.taskId];
      set({ queue: get().queue.filter((task) => task.id !== event.taskId), speedByTaskId, etaByTaskId });
      void get().fetchHistory();
      return;
    }

    const { queue } = get();
    const index = queue.findIndex((task) => task.id === event.taskId);
    // Narrow race: the event arrived before the local `fetchQueue()` that
    // follows a `queueUpload`/`queueDownload` call finished populating this
    // task. Harmless to drop - progress events fire roughly every 500ms, so
    // the next one lands after the task exists in `queue`.
    if (index === -1) return;

    const next = queue.slice();
    next[index] = {
      ...next[index],
      transferredBytes: event.transferredBytes,
      totalBytes: event.totalBytes,
      status: event.status,
      errorMessage: event.error,
    };
    set({
      queue: next,
      speedByTaskId: { ...get().speedByTaskId, [event.taskId]: event.speedBytesPerSec },
      etaByTaskId: { ...get().etaByTaskId, [event.taskId]: event.etaSeconds },
    });
  },
}));
