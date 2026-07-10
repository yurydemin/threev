import { useEffect } from 'react';
import { useAppStore } from '../stores/useAppStore';

/**
 * Applies the app-wide UI scale preference (90/100/110/125%, see
 * `AppearanceSection`) to the whole document.
 *
 * Uses CSS `zoom` (not `transform: scale`) — per the Stage 4 plan's agreed
 * decision, `zoom` rescales layout (so scrollbars/hit-testing/`vh`/`vw`
 * stay correct at non-100% values), which `transform: scale` does not do
 * without extra compensation. `zoom` is WebKit-only in the general web, but
 * Wails' frontend runtime is always WebKit-based (macOS WKWebView / Windows
 * WebView2 / Linux WebKitGTK), so that's not a portability concern here.
 *
 * Same "safe to call from multiple components" contract as `useTheme`: the
 * preference lives in `useAppStore` (persisted), so all callers stay in
 * sync.
 */
export function useUIScale() {
  const uiScalePercent = useAppStore((state) => state.uiScalePercent);
  const setUiScalePercent = useAppStore((state) => state.setUiScalePercent);

  useEffect(() => {
    // `zoom` isn't part of the standard CSSStyleDeclaration typings (it's a
    // WebKit-only property), so it's set via `setProperty` rather than a
    // direct `.zoom =` assignment, which TypeScript would reject.
    document.documentElement.style.setProperty('zoom', `${uiScalePercent}%`);
  }, [uiScalePercent]);

  return { uiScalePercent, setUiScalePercent };
}
