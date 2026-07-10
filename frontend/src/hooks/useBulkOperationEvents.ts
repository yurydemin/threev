import { useEffect } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { useBulkOperationStore } from '../stores/useBulkOperationStore';
import type { BulkOperationProgressEvent } from '../types';

const BULK_PROGRESS_EVENT = 'bulk:progress';

/**
 * Maps the raw payload of the "bulk:progress" Wails event into the
 * frontend-domain `BulkOperationProgressEvent` shape. There is no generated
 * `domain.BulkOperationProgressEvent` binding to convert from (`wails
 * generate module` only scans bound service method signatures, not
 * `runtime.EventsEmit` call sites) — same caveat as
 * `hooks/useTransferEvents.ts`'s `mapProgressEvent`.
 */
function mapBulkProgressEvent(raw: any): BulkOperationProgressEvent {
  return {
    operationId: raw.OperationID,
    type: raw.Type,
    total: raw.Total,
    completed: raw.Completed,
    failedCount: raw.FailedCount,
    status: raw.Status,
  };
}

/**
 * Wires the "bulk:progress" Wails event into `useBulkOperationStore`.
 *
 * Unlike `useTransferEvents` (mounted once, globally, in `App.tsx`), this is
 * mounted locally in `FileManagerScreen`: bulk delete/copy/move operations
 * only matter while the user is looking at the File Manager — there is no
 * always-visible status-bar indicator for them (unlike background
 * transfers) — so there's nothing to keep in sync outside that screen.
 * Nothing is hydrated on mount either: `active` starts `null` and only ever
 * becomes non-null once the user actually starts an operation from this
 * screen (`useBulkOperationStore`'s `start*` actions), so there is no
 * server-side state to fetch up front the way `useTransferEvents` fetches
 * the queue/history.
 */
export function useBulkOperationEvents(): void {
  useEffect(() => {
    const off = EventsOn(BULK_PROGRESS_EVENT, (raw: any) => {
      useBulkOperationStore.getState().applyProgressEvent(mapBulkProgressEvent(raw));
    });

    return () => {
      off();
    };
  }, []);
}
