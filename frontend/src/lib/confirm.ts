import { useConfirmStore, type ConfirmOptions } from '../stores/useConfirmStore';

/**
 * Thin wrapper around `useConfirmStore` for call sites outside React
 * components/hooks (event handlers, store actions, `lib/*` helpers) — same
 * `useXStore.getState().xxx()` convention as `lib/toast.ts`.
 *
 * Resolves `true` on Confirm, `false` on Cancel/X/backdrop/Esc — see
 * `useConfirmStore`'s doc comment for the full rationale (replaces
 * `window.confirm`, which doesn't work in the packaged WKWebView app).
 */
export function confirmDialog(message: string, options?: Omit<ConfirmOptions, 'message'>): Promise<boolean> {
  return useConfirmStore.getState().request({ message, ...options });
}
