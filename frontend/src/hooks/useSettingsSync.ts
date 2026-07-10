import { useEffect } from 'react';
import { useSettingsStore } from '../stores/useSettingsStore';
import { useAppStore, type ThemePreference } from '../stores/useAppStore';

/**
 * Reconciles `useAppStore`'s localStorage-persisted theme/UI-scale
 * "quick guess" with `appsettings.SettingsService`'s backend-persisted
 * values, per `useAppStore`'s own doc-comment.
 *
 * Fetches settings once on mount (`useSettingsStore.fetchSettings`) —
 * mirrors `useTransferEvents`' "mount once at the `App.tsx` root,
 * regardless of which screen is active" placement, since theme/scale are
 * relevant everywhere, not just on the Settings screen. When the fetch
 * resolves and `settings` differs from the current `useAppStore` value, the
 * backend value wins and overwrites the store (which also re-persists it to
 * localStorage via `useAppStore`'s own `persist` middleware, keeping both in
 * sync for the next cold start before this hook gets a chance to run).
 *
 * This hook does not itself apply theme/scale to the DOM — that's
 * `useTheme`/`useUIScale`'s job, both of which already react to
 * `useAppStore` changes on their own.
 */
export function useSettingsSync() {
  const settings = useSettingsStore((state) => state.settings);

  useEffect(() => {
    void useSettingsStore.getState().fetchSettings();
    // Runs once, on mount, regardless of which screen is active — same
    // rationale as `useTransferEvents`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!settings) return;

    const { theme, setTheme, uiScalePercent, setUiScalePercent } = useAppStore.getState();
    if (settings.theme !== theme) {
      setTheme(settings.theme as ThemePreference);
    }
    if (settings.uiScalePercent !== uiScalePercent) {
      setUiScalePercent(settings.uiScalePercent);
    }
  }, [settings]);
}
