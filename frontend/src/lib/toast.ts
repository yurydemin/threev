import { useToastStore } from '../stores/useToastStore';

/**
 * Thin wrapper around `useToastStore` for call sites outside React
 * components/hooks (event handlers, store actions, `lib/*` helpers) — same
 * `useXStore.getState().xxx()` convention already used throughout the app
 * (e.g. `useTransferStore.getState().queueDownload(...)`).
 *
 * Inside a component, prefer subscribing to `useToastStore` directly if you
 * need reactive state; `toast.*` is for fire-and-forget notifications.
 */
export const toast = {
  success: (message: string) => useToastStore.getState().show('success', message),
  error: (message: string) => useToastStore.getState().show('error', message),
  warning: (message: string) => useToastStore.getState().show('warning', message),
  info: (message: string) => useToastStore.getState().show('info', message),
};
