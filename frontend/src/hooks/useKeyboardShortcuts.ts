import { useEffect } from 'react';

export interface UseKeyboardShortcutsOptions {
  /** Invoked on Ctrl+R (Windows/Linux) / Cmd+R (macOS). */
  onRefresh: () => void;
  /** Invoked on Ctrl+A (Windows/Linux) / Cmd+A (macOS). */
  onSelectAll: () => void;
  /** Invoked on Escape. */
  onClearSelection: () => void;
}

/**
 * Global keyboard shortcuts for the File Manager screen (Stage 2, Block I;
 * extended in Stage 4, Block C with Ctrl/Cmd+A and Escape for bulk select).
 * More shortcuts (F2 rename, Delete, ...) belong to Stage 4, Block D once
 * their underlying actions (the confirm/rename modals) exist, per the
 * "don't offer a non-working feature" pattern used throughout this project.
 *
 * `preventDefault()` is required even inside the Wails WebView: without it,
 * Ctrl/Cmd+R falls through to the browser engine's own page-reload shortcut
 * (which would blow away all in-memory app state), and Ctrl/Cmd+A falls
 * through to "select all text on the page".
 *
 * Escape is *not* handled by `Modal.tsx` here — Headless UI's `Dialog`
 * already closes itself on Escape internally, so `onClearSelection` only
 * ever fires when no modal is open to intercept it first.
 */
export function useKeyboardShortcuts({ onRefresh, onSelectAll, onClearSelection }: UseKeyboardShortcutsOptions): void {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'r') {
        event.preventDefault();
        onRefresh();
        return;
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'a') {
        event.preventDefault();
        onSelectAll();
        return;
      }
      if (event.key === 'Escape') {
        onClearSelection();
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onRefresh, onSelectAll, onClearSelection]);
}
