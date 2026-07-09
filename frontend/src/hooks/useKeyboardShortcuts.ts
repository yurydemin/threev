import { useEffect } from 'react';

export interface UseKeyboardShortcutsOptions {
  /** Invoked on Ctrl+R (Windows/Linux) / Cmd+R (macOS). */
  onRefresh: () => void;
}

/**
 * Global keyboard shortcuts for the File Manager screen (Stage 2, Block I).
 * Currently just Ctrl/Cmd+R → refresh (per the plan's step 39); more
 * shortcuts (F2 rename, Delete, ...) belong to later stages once their
 * underlying actions exist, per the "don't offer a non-working feature"
 * pattern used throughout Stage 2.
 *
 * `preventDefault()` is required even inside the Wails WebView: without it,
 * Ctrl/Cmd+R falls through to the browser engine's own page-reload shortcut,
 * which would blow away all in-memory app state.
 */
export function useKeyboardShortcuts({ onRefresh }: UseKeyboardShortcutsOptions): void {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'r') {
        event.preventDefault();
        onRefresh();
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onRefresh]);
}
