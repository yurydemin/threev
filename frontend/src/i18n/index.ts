import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import ru from './locales/ru.json';
import en from './locales/en.json';

/**
 * `react-i18next` setup (Stage 4, Block K). Language is a pure frontend
 * preference — stored in `useAppStore` (localStorage), same as `theme`/
 * `uiScalePercent` — NOT part of `domain.AppSettings` on the backend. See
 * `useLanguageSync` for how `useAppStore`'s `language` drives this
 * instance's active language.
 *
 * No `i18next-browser-languagedetector`: `useAppStore` is already the single
 * source of truth for the active language, a separate detector plugin would
 * just be a second one. Default language is `'ru'` (matches the app's
 * pre-i18n text), no `navigator.language` auto-detection.
 */
void i18n.use(initReactI18next).init({
  resources: {
    ru: { translation: ru },
    en: { translation: en },
  },
  lng: 'ru',
  fallbackLng: 'ru',
  interpolation: {
    escapeValue: false, // React already escapes interpolated values.
  },
});

export default i18n;
