import { create } from 'zustand';

export interface ConfirmOptions {
  /** Body text. The only required field — everything else falls back to a generic `common.*` label in `ConfirmDialog`. */
  message: string;
  /** Modal title. Defaults to `common.confirmTitle`. */
  title?: string;
  /** Confirm button label. Defaults to `common.confirm`. */
  confirmLabel?: string;
  /** Cancel button label. Defaults to `common.cancel`. */
  cancelLabel?: string;
  /** Renders the confirm button as `variant="danger"` instead of `variant="primary"` — for destructive actions (delete, discard, ...). */
  danger?: boolean;
}

interface ConfirmState {
  isOpen: boolean;
  options: ConfirmOptions | null;
  resolve: ((result: boolean) => void) | null;
  /** Opens the dialog and returns a promise that settles once the user answers — see the module doc comment below for the calling convention. */
  request: (options: ConfirmOptions) => Promise<boolean>;
  /** Confirm button (or any other "yes" action) — resolves the pending promise with `true`. */
  handleConfirm: () => void;
  /** Cancel button, the Modal's own X button, backdrop click, or Esc — all funnel through `Modal`'s `onClose`, so all of them must resolve `false`, same as an explicit Cancel. */
  handleCancel: () => void;
}

/**
 * Global imperative confirmation dialog, replacing `window.confirm`.
 *
 * Wails' WKWebView backend on macOS doesn't implement the native JS
 * confirm/alert panel unless the host app explicitly wires up a
 * `WKUIDelegate` (which this project doesn't) — in the packaged app,
 * `window.confirm(...)` silently does nothing and returns a falsy value
 * immediately, which silently skipped every destructive action gated behind
 * it. This store backs a real, React-rendered dialog (`ConfirmDialog`,
 * mounted once at the app root, same as `ToastContainer`) that works
 * regardless of native WebView dialog support.
 *
 * Unlike `useToastStore` (fire-and-forget), a confirmation needs to hand the
 * caller back an answer, so `request(...)` wraps the open/close bookkeeping
 * in a `Promise<boolean>` that only settles once the user picks
 * Confirm/Cancel (or closes the dialog by any other means — X button,
 * backdrop click, Esc — all of which are treated as Cancel). This lets call
 * sites outside React's render (event handlers, store actions) `await` an
 * answer exactly like they would `window.confirm`'s return value, just
 * asynchronously:
 *
 * ```ts
 * import { confirmDialog } from '../lib/confirm';
 *
 * async function handleDelete() {
 *   if (!(await confirmDialog(t('...')))) return;
 *   await deleteThing();
 * }
 * ```
 *
 * Same `useXStore.getState().xxx()` convention as `useToastStore`/
 * `useTransferStore` for code that isn't already subscribed to the store via
 * the hook — see `lib/confirm.ts`, the thin wrapper call sites should
 * actually import.
 */
export const useConfirmStore = create<ConfirmState>()((set, get) => ({
  isOpen: false,
  options: null,
  resolve: null,

  request: (options) =>
    new Promise<boolean>((resolve) => {
      set({ isOpen: true, options, resolve });
    }),

  handleConfirm: () => {
    get().resolve?.(true);
    set({ isOpen: false, options: null, resolve: null });
  },

  handleCancel: () => {
    get().resolve?.(false);
    set({ isOpen: false, options: null, resolve: null });
  },
}));
