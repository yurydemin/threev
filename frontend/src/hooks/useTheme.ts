import { useEffect } from 'react';
import { useAppStore, type ThemePreference } from '../stores/useAppStore';

export type ResolvedTheme = 'light' | 'dark';

const LIGHT_MEDIA_QUERY = '(prefers-color-scheme: light)';

function resolveTheme(preference: ThemePreference, systemPrefersLight: boolean): ResolvedTheme {
  if (preference === 'light') return 'light';
  if (preference === 'dark') return 'dark';
  return systemPrefersLight ? 'light' : 'dark';
}

function applyTheme(resolved: ResolvedTheme): void {
  const root = document.documentElement;
  if (resolved === 'light') {
    root.classList.add('light');
  } else {
    root.classList.remove('light');
  }
}

/**
 * Reads/applies the app-wide theme preference (System/Light/Dark).
 *
 * - "system" tracks `prefers-color-scheme` live via matchMedia.
 * - Resolved theme is applied as a `.light` class on `<html>` (dark is the
 *   implicit default defined on `:root`, per docs/03-ux-ui-spec.md section 11).
 *
 * This hook is safe to call from multiple components; the underlying
 * preference lives in `useAppStore` (persisted), so all callers stay in sync.
 */
export function useTheme() {
  const theme = useAppStore((state) => state.theme);
  const setTheme = useAppStore((state) => state.setTheme);

  useEffect(() => {
    const mediaQuery = window.matchMedia(LIGHT_MEDIA_QUERY);

    const apply = () => {
      applyTheme(resolveTheme(theme, mediaQuery.matches));
    };

    apply();

    if (theme !== 'system') {
      return;
    }

    mediaQuery.addEventListener('change', apply);
    return () => mediaQuery.removeEventListener('change', apply);
  }, [theme]);

  return { theme, setTheme };
}
