import { useEffect } from 'react';

export interface UseKeyboardShortcutsOptions {
  /** Invoked on Ctrl+R (Windows/Linux) / Cmd+R (macOS). */
  onRefresh: () => void;
  /** Invoked on Ctrl+A (Windows/Linux) / Cmd+A (macOS). */
  onSelectAll: () => void;
  /** Invoked on Escape. */
  onClearSelection: () => void;
  /** Invoked on Delete/Backspace (Stage 4 Block D). Expected to no-op itself if nothing is selected. */
  onDeleteSelected: () => void;
  /** Invoked on F2 (Stage 4 Block D). Expected to no-op itself unless exactly one object is selected. */
  onRenameSelected: () => void;
}

/**
 * Global keyboard shortcuts for the File Manager screen (Stage 2, Block I;
 * extended in Stage 4, Block C with Ctrl/Cmd+A and Escape for bulk select,
 * and Block D with Delete/Backspace and F2 now that their underlying
 * actions — the confirm/rename modals — exist).
 *
 * `preventDefault()` is required even inside the Wails WebView: without it,
 * Ctrl/Cmd+R falls through to the browser engine's own page-reload shortcut
 * (which would blow away all in-memory app state), and Ctrl/Cmd+A falls
 * through to "select all text on the page".
 *
 * Escape is *not* handled by `Modal.tsx` here — Headless UI's `Dialog`
 * already closes itself on Escape internally, so `onClearSelection` only
 * ever fires when no modal is open to intercept it first.
 *
 * Every shortcut is skipped entirely whenever `event.target` is a text
 * input/textarea, checked once at the very top of `handleKeyDown` — added
 * in Stage 4 Block D alongside Delete/Backspace (without it, pressing
 * Delete/Backspace to edit the Toolbar search field, or any modal's `Input`,
 * would instead pop a delete-confirmation dialog). This also fixes a
 * latent Block C bug: Ctrl/Cmd+A typed into the search field previously
 * selected every object in the list instead of the field's text, and
 * Escape inside a text field cleared the object selection instead of just
 * losing focus — both are silently fixed by the same single guard, since
 * neither was ever the intended behavior while typing.
 */
export function useKeyboardShortcuts({
  onRefresh,
  onSelectAll,
  onClearSelection,
  onDeleteSelected,
  onRenameSelected,
}: UseKeyboardShortcutsOptions): void {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;

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
        return;
      }
      // On macOS the physical "Delete" (backspace) key reports `event.key
      // === "Backspace"` — that's the Web API's actual naming, not a typo
      // here; forward-delete reports "Delete". Both are treated the same.
      if (event.key === 'Delete' || event.key === 'Backspace') {
        event.preventDefault();
        onDeleteSelected();
        return;
      }
      if (event.key === 'F2') {
        onRenameSelected();
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onRefresh, onSelectAll, onClearSelection, onDeleteSelected, onRenameSelected]);
}
