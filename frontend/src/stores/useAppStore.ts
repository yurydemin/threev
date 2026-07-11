import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ThemePreference = 'system' | 'light' | 'dark';

/**
 * UI language. Purely a frontend preference (see `useLanguageSync`) — there
 * is no backing field in `domain.AppSettings`, unlike `theme`/
 * `uiScalePercent` which are reconciled with the backend via
 * `useSettingsSync`.
 */
export type Language = 'ru' | 'en';

interface AppState {
  theme: ThemePreference;
  /** UI zoom level, 90-125 (see `useUIScale`). Defaults to 100 (unscaled). */
  uiScalePercent: number;
  sidebarCollapsed: boolean;
  /** UI language (see `useLanguageSync`). Defaults to 'ru'. */
  language: Language;
  setTheme: (theme: ThemePreference) => void;
  setUiScalePercent: (percent: number) => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebarCollapsed: () => void;
  setLanguage: (language: Language) => void;
}

/**
 * Global application-level UI state (theme preference, UI scale, sidebar
 * layout, ...).
 *
 * `theme`/`uiScalePercent` are persisted to localStorage as a "quick guess"
 * so the UI doesn't flash defaults before the backend answers — Settings
 * (Stage 4, Block G) reconciles both with `appsettings.SettingsService`'s
 * persisted values on startup via `useSettingsSync`, which becomes the
 * source of truth from that point on (see that hook's doc-comment).
 */
export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      theme: 'system',
      uiScalePercent: 100,
      sidebarCollapsed: false,
      language: 'ru',
      setTheme: (theme) => set({ theme }),
      setUiScalePercent: (uiScalePercent) => set({ uiScalePercent }),
      setSidebarCollapsed: (sidebarCollapsed) => set({ sidebarCollapsed }),
      toggleSidebarCollapsed: () =>
        set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setLanguage: (language) => set({ language }),
    }),
    {
      name: 'threev-app-store',
      partialize: (state) => ({
        theme: state.theme,
        uiScalePercent: state.uiScalePercent,
        sidebarCollapsed: state.sidebarCollapsed,
        language: state.language,
      }),
    },
  ),
);
