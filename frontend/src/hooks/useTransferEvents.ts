import { useEffect } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { useFileManagerStore } from '../stores/useFileManagerStore';
import { useTransferStore } from '../stores/useTransferStore';
import type { ObjectChangeEvent, TransferProgressEvent } from '../types';

const TRANSFER_PROGRESS_EVENT = 'transfer:progress';
const OBJECT_CHANGE_EVENT = 'object:change';

/**
 * Maps the raw payload of the "transfer:progress" Wails event into the
 * frontend-domain `TransferProgressEvent` shape. There is no generated
 * `domain.TransferProgressEvent` binding to convert from (`wails generate
 * module` only scans bound service method signatures, not
 * `runtime.EventsEmit` call sites - see `lib/wails/transfer.ts`), so
 * `raw` is a plain PascalCase object mirroring the Go struct one-to-one.
 */
function mapProgressEvent(raw: any): TransferProgressEvent {
  return {
    taskId: raw.TaskID,
    transferredBytes: raw.TransferredBytes,
    totalBytes: raw.TotalBytes,
    speedBytesPerSec: raw.SpeedBytesPerSec,
    etaSeconds: raw.ETASeconds,
    status: raw.Status,
    error: raw.Error,
  };
}

/** Maps the raw payload of the "object:change" Wails event. Same caveat as `mapProgressEvent`. */
function mapObjectChangeEvent(raw: any): ObjectChangeEvent {
  return {
    bucket: raw.Bucket,
    prefix: raw.Prefix,
    type: raw.Type,
  };
}

/**
 * Wires the Transfer Engine's Wails events ("transfer:progress",
 * "object:change") into `useTransferStore`/`useFileManagerStore`, and
 * hydrates the initial queue/history on mount.
 *
 * This hook is meant to be mounted once for the app's whole lifetime (at
 * the `App.tsx` level, added in Stage 3 Block K) rather than from the
 * Transfers screen itself: a status-bar transfer indicator (also Block K)
 * needs an up-to-date `queue` even if the user never opens that screen, and
 * mounting the event subscriptions exactly once avoids duplicate listeners
 * that per-screen mounting would require careful cleanup to avoid.
 *
 * Returns nothing - it is a pure side-effect hook, called as
 * `useTransferEvents();`.
 */
export function useTransferEvents(): void {
  useEffect(() => {
    void useTransferStore.getState().fetchQueue();
    void useTransferStore.getState().fetchHistory();

    const offProgress = EventsOn(TRANSFER_PROGRESS_EVENT, (raw: any) => {
      useTransferStore.getState().applyProgressEvent(mapProgressEvent(raw));
    });

    const offObjectChange = EventsOn(OBJECT_CHANGE_EVENT, (raw: any) => {
      const event = mapObjectChangeEvent(raw);
      const fileManagerState = useFileManagerStore.getState();
      if (fileManagerState.selectedBucket === event.bucket && fileManagerState.currentPrefix === event.prefix) {
        void fileManagerState.refresh();
      }
    });

    return () => {
      offProgress();
      offObjectChange();
    };
  }, []);
}
