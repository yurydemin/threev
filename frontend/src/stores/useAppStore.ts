import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ThemePreference = 'system' | 'light' | 'dark';

interface AppState {
  theme: ThemePreference;
  sidebarCollapsed: boolean;
  setTheme: (theme: ThemePreference) => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebarCollapsed: () => void;
}

/**
 * Global application-level UI state (theme preference, sidebar layout, ...).
 *
 * Persisted to localStorage for Stage 1. Once Settings (Stage 4) is
 * implemented, theme preference will move to backend-persisted settings and
 * this store will be reconciled with it.
 */
export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      theme: 'system',
      sidebarCollapsed: false,
      setTheme: (theme) => set({ theme }),
      setSidebarCollapsed: (sidebarCollapsed) => set({ sidebarCollapsed }),
      toggleSidebarCollapsed: () =>
        set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
    }),
    {
      name: 'threev-app-store',
      partialize: (state) => ({
        theme: state.theme,
        sidebarCollapsed: state.sidebarCollapsed,
      }),
    },
  ),
);
