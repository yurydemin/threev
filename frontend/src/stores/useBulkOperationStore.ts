import { create } from 'zustand';
import { cancelBulkOperation, copyObjects, deleteObjects, moveObjects } from '../lib/wails/fileManager';
import { ApiError } from '../lib/wails/errors';
import { toast } from '../lib/toast';
import i18n from '../i18n';
import type { BulkOperationProgressEvent } from '../types';

/** `BulkOperationProgressEvent.status` values that mean "no longer running". */
const TERMINAL_STATUSES = new Set(['completed', 'cancelled']);

/** How long a finished operation's progress row stays visible before auto-hiding (UX spec 5.8.4). */
const AUTO_HIDE_MS = 3000;

interface BulkOperationState {
  /**
   * The bulk operation `BulkProgressOverlay` currently shows, or `null` if
   * none. Bulk operations can technically run concurrently on the backend
   * (each gets its own operation id — see `internal/filemanager/bulkops.go`),
   * but the UI only ever surfaces one progress row at a time (UX spec
   * 5.8.4): starting a new operation while another is still `active` simply
   * replaces what's shown, it does not queue.
   */
  active: BulkOperationProgressEvent | null;
  startDelete: (profileId: number, bucket: string, keys: string[]) => Promise<void>;
  startCopy: (
    profileId: number,
    sourceBucket: string,
    keys: string[],
    destBucket: string,
    destPrefix: string,
  ) => Promise<void>;
  startMove: (
    profileId: number,
    sourceBucket: string,
    keys: string[],
    destBucket: string,
    destPrefix: string,
  ) => Promise<void>;
  /** Cancels the currently `active` operation, if any (no-op otherwise). */
  cancel: () => Promise<void>;
  /** Applies a "bulk:progress" event (see `hooks/useBulkOperationEvents.ts`). */
  applyProgressEvent: (event: BulkOperationProgressEvent) => void;
  /** Manually hides the progress row before its `AUTO_HIDE_MS` timer fires. */
  dismiss: () => void;
}

/**
 * Tracks the single bulk delete/copy/move operation `BulkProgressOverlay`
 * displays, per docs/03-ux-ui-spec.md section 5.8.4.
 *
 * `start*` set `active` to an optimistic `status: 'running'` snapshot
 * immediately after the backend hands back an operation id — *before* the
 * first real "bulk:progress" event arrives — so the overlay appears
 * instantly instead of flashing empty for one event round-trip.
 * `applyProgressEvent` then keeps it in sync with real backend state, and
 * schedules the auto-hide once a terminal status is observed.
 */
export const useBulkOperationStore = create<BulkOperationState>()((set, get) => {
  function scheduleAutoHide(operationId: number) {
    setTimeout(() => {
      if (get().active?.operationId === operationId) {
        set({ active: null });
      }
    }, AUTO_HIDE_MS);
  }

  return {
    active: null,

    startDelete: async (profileId, bucket, keys) => {
      let operationId: number;
      try {
        operationId = await deleteObjects(profileId, bucket, keys);
      } catch (err) {
        console.error('[useBulkOperationStore] deleteObjects failed:', err);
        toast.error(
          err instanceof ApiError ? err.message : i18n.t('fileManager.bulkOperationStore.deleteStartError'),
          err instanceof ApiError ? err.raw : undefined,
        );
        return;
      }
      set({
        active: {
          operationId,
          type: 'delete',
          total: keys.length,
          completed: 0,
          failedCount: 0,
          status: 'running',
        },
      });
    },

    startCopy: async (profileId, sourceBucket, keys, destBucket, destPrefix) => {
      let operationId: number;
      try {
        operationId = await copyObjects(profileId, sourceBucket, keys, destBucket, destPrefix);
      } catch (err) {
        console.error('[useBulkOperationStore] copyObjects failed:', err);
        toast.error(
          err instanceof ApiError ? err.message : i18n.t('fileManager.bulkOperationStore.copyStartError'),
          err instanceof ApiError ? err.raw : undefined,
        );
        return;
      }
      set({
        active: {
          operationId,
          type: 'copy',
          total: keys.length,
          completed: 0,
          failedCount: 0,
          status: 'running',
        },
      });
    },

    startMove: async (profileId, sourceBucket, keys, destBucket, destPrefix) => {
      let operationId: number;
      try {
        operationId = await moveObjects(profileId, sourceBucket, keys, destBucket, destPrefix);
      } catch (err) {
        console.error('[useBulkOperationStore] moveObjects failed:', err);
        toast.error(
          err instanceof ApiError ? err.message : i18n.t('fileManager.bulkOperationStore.moveStartError'),
          err instanceof ApiError ? err.raw : undefined,
        );
        return;
      }
      set({
        active: {
          operationId,
          type: 'move',
          total: keys.length,
          completed: 0,
          failedCount: 0,
          status: 'running',
        },
      });
    },

    cancel: async () => {
      const { active } = get();
      if (!active) return;
      try {
        await cancelBulkOperation(active.operationId);
      } catch (err) {
        console.error('[useBulkOperationStore] cancelBulkOperation failed:', err);
        toast.error(
          err instanceof ApiError ? err.message : i18n.t('fileManager.bulkOperationStore.cancelError'),
          err instanceof ApiError ? err.raw : undefined,
        );
      }
    },

    applyProgressEvent: (event) => {
      if (event.operationId !== get().active?.operationId) return;
      set({ active: event });
      if (TERMINAL_STATUSES.has(event.status)) {
        scheduleAutoHide(event.operationId);
        if (event.failedCount > 0) {
          toast.warning(
            i18n.t('fileManager.bulkOperationStore.partialFailure', {
              failed: event.failedCount,
              total: event.total,
            }),
          );
        }
      }
    },

    dismiss: () => set({ active: null }),
  };
});
