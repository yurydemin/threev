import { useEffect } from 'react';
import i18n from '../i18n';
import { useAppStore } from '../stores/useAppStore';

/**
 * Applies the app-wide UI language preference (see `GeneralSection`'s
 * `LanguageSwitcher`) to the `react-i18next` singleton.
 *
 * Same "safe to call from multiple components, single source of truth in
 * `useAppStore`" contract as `useTheme`/`useUIScale` — mounted once at the
 * `App.tsx` root.
 */
export function useLanguageSync() {
  const language = useAppStore((state) => state.language);

  useEffect(() => {
    void i18n.changeLanguage(language);
  }, [language]);
}
