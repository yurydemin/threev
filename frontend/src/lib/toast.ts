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
  /**
   * `details`, when given, is the technical `ApiError.raw` behind `message`
   * (UX-007) — `Toast.tsx` renders a "Скопировать детали" button for it.
   * Not offered on `success`/`warning`/`info`: only `ApiError`-driven
   * failures ever have technical details worth copying.
   */
  error: (message: string, details?: string) => useToastStore.getState().show('error', message, details),
  warning: (message: string) => useToastStore.getState().show('warning', message),
  info: (message: string) => useToastStore.getState().show('info', message),
};
