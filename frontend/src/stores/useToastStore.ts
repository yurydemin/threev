import { create } from 'zustand';

export type ToastType = 'success' | 'error' | 'warning' | 'info';

export interface ToastItem {
  id: number;
  type: ToastType;
  message: string;
  /** Technical details behind `message` (UX-007's "Скопировать детали") — only ever set for `type === 'error'`. */
  details?: string;
}

interface ToastState {
  toasts: ToastItem[];
  show: (type: ToastType, message: string, details?: string) => void;
  dismiss: (id: number) => void;
}

/** `success`/`info` auto-hide after 5s, `error`/`warning` after 10s (UX spec 4.8). */
const SHORT_DURATION_MS = 5000;
const LONG_DURATION_MS = 10000;

/**
 * Global toast/notification queue, per docs/03-ux-ui-spec.md section 4.8.
 *
 * Not part of `types/index.ts` — toasts are a pure frontend presentation
 * concern with no backend DTO counterpart (unlike e.g.
 * `BulkOperationProgressEvent`).
 *
 * Consumed by `components/ui/ToastContainer.tsx` (rendering) and driven by
 * `lib/toast.ts` (the `useToastStore.getState().show(...)` call sites use,
 * same `getState()` convention as `useTransferStore`/`useBulkOperationStore`
 * for code outside React components/event handlers that aren't already
 * subscribed via the hook).
 */
export const useToastStore = create<ToastState>()((set, get) => {
  let nextId = 0;

  return {
    toasts: [],

    show: (type, message, details) => {
      const id = nextId++;
      set((state) => ({ toasts: [...state.toasts, { id, type, message, details }] }));
      // `warning` is grouped with `error` (10s) rather than `success`/`info`
      // (5s): the spec only explicitly calls out 5s for success/info and 10s
      // for error, but a warning — like an error — flags something the user
      // should actually read (e.g. a partial bulk-operation failure, Block
      // E's `applyProgressEvent` hookup), so it gets the same longer window.
      const duration = type === 'success' || type === 'info' ? SHORT_DURATION_MS : LONG_DURATION_MS;
      setTimeout(() => get().dismiss(id), duration);
    },

    dismiss: (id) => set((state) => ({ toasts: state.toasts.filter((toast) => toast.id !== id) })),
  };
});
