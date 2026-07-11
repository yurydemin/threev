import { create } from 'zustand';
import { getSettings, saveSettings as apiSaveSettings } from '../lib/wails/appsettings';
import { ApiError } from '../lib/wails/errors';
import { toast } from '../lib/toast';
import i18n from '../i18n';
import type { AppSettings } from '../types';

interface SettingsState {
  settings: AppSettings | null;
  isLoading: boolean;
  error: string | null;

  fetchSettings: () => Promise<void>;
  /** Returns `true` on success, `false` on failure (see `error` for the message). */
  saveSettings: (settings: AppSettings) => Promise<boolean>;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}

/**
 * App settings state, backed by `appsettings.SettingsService` via
 * `lib/wails/appsettings.ts`.
 *
 * `settings` starts `null` and is only populated once `fetchSettings()`
 * resolves — see `useSettingsSync` for how that first fetch reconciles
 * `useAppStore`'s localStorage-persisted theme/UI-scale "quick guess" with
 * the backend's persisted values, which become the source of truth from
 * that point on.
 */
export const useSettingsStore = create<SettingsState>()((set) => ({
  settings: null,
  isLoading: false,
  error: null,

  fetchSettings: async () => {
    set({ isLoading: true, error: null });
    try {
      const settings = await getSettings();
      set({ settings, isLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), isLoading: false });
    }
  },

  saveSettings: async (settings) => {
    set({ isLoading: true, error: null });
    try {
      await apiSaveSettings(settings);
      set({ settings, isLoading: false });
      toast.success(i18n.t('settings.settingsStore.saved'));
      return true;
    } catch (err) {
      const message = errorMessage(err);
      set({ error: message, isLoading: false });
      toast.error(message || i18n.t('settings.settingsStore.saveError'), err instanceof ApiError ? err.raw : undefined);
      return false;
    }
  },
}));
