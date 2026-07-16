import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Download, Upload } from 'lucide-react';
import { Sidebar } from '../components/layout/Sidebar';
import { ConnectionForm } from '../components/connection/ConnectionForm';
import { ConnectionList } from '../components/connection/ConnectionList';
import { Button } from '../components/ui/Button';
import { getConnection, exportProfiles, importProfiles } from '../lib/wails/connection';
import { cancelTasksForProfile } from '../lib/wails/transfer';
import { confirmDialog } from '../lib/confirm';
import { toast } from '../lib/toast';
import { ApiError } from '../lib/wails/errors';
import { useConnectionStore } from '../stores/useConnectionStore';
import { useFavoritesStore } from '../stores/useFavoritesStore';
import { useFileManagerStore } from '../stores/useFileManagerStore';
import { useTransferStore } from '../stores/useTransferStore';
import type { Connection, ConnectionSummary, Favorite } from '../types';

type FormState = { open: false } | { open: true; initialValues?: Connection };

export interface ConnectionsScreenProps {
  /** Enters the File Manager for this connection ("Подключиться" on a card). */
  onConnect: (connection: ConnectionSummary) => void;
  /** Navigates to the Transfers screen (Sidebar "Передачи"). */
  onSelectTransfers: () => void;
  /** Navigates to the History screen (Sidebar "История"). */
  onSelectHistory: () => void;
  /** Navigates to the Settings screen (Sidebar "Настройки"). */
  onSelectSettings: () => void;
  /** Returns to an already-open File Manager session (Sidebar active-connection indicator, Block L2). */
  onSelectFileManager: () => void;
  /** Closes the open File Manager session (Sidebar active-connection indicator's "X" button). */
  onDisconnect: () => void;
  /** Handles a click on a Sidebar favorites-section row. */
  onSelectFavorite: (favorite: Favorite) => void;
}

/**
 * "Список подключений" per docs/03-ux-ui-spec.md section 5.2.
 *
 * Export/Import (Block G) live in the header, separate from the centered
 * "+ Добавить подключение" button below the grid: they're profile-list-wide
 * actions, not "add one connection" — see `handleExport`/`handleImport`.
 *
 * "Дублировать" is an instant action (no intermediate modal): it re-saves
 * the fetched record with `id: 0` (create-new, per `SaveProfile` semantics)
 * and a "(копия)" suffix. Delete goes through `confirmDialog`
 * (`components/ui/ConfirmDialog.tsx`), a React-rendered confirmation dialog
 * — not `window.confirm`, which silently no-ops in the packaged WKWebView
 * app (Wails' macOS webview doesn't implement the native confirm/alert
 * panel without an explicit `WKUIDelegate`, which this project doesn't
 * wire up).
 *
 * The card menu's "Тестировать" opens the same edit modal as
 * "Редактировать" (rather than testing silently in the background) — there
 * is no toast/notification system yet to surface a background test result,
 * so routing through the form's own test UI is the honest choice.
 */
export function ConnectionsScreen({
  onConnect,
  onSelectTransfers,
  onSelectHistory,
  onSelectSettings,
  onSelectFileManager,
  onDisconnect,
  onSelectFavorite,
}: ConnectionsScreenProps) {
  const { t } = useTranslation();
  const connections = useConnectionStore((state) => state.connections);
  const isLoading = useConnectionStore((state) => state.isLoading);
  const deleteConnection = useConnectionStore((state) => state.deleteConnection);

  const [formState, setFormState] = useState<FormState>({ open: false });
  const [isExporting, setIsExporting] = useState(false);
  const [isImporting, setIsImporting] = useState(false);

  function openCreate() {
    setFormState({ open: true, initialValues: undefined });
  }

  // UX-004: Ctrl/Cmd+N opens the "new connection" form. Scoped to this
  // screen's own `useEffect` rather than `useKeyboardShortcuts` — that hook
  // is documented as File Manager-specific (different shortcut set, mounted
  // only in `FileManagerScreen`), and adding a second, unrelated options
  // object to it for one screen's one shortcut isn't worth the coupling.
  // Same text-field guard as `useKeyboardShortcuts` (Stage 4 Block D): skips
  // entirely while the target is an `<input>`/`<textarea>`, e.g. so Ctrl/Cmd+N
  // while typing in `ConnectionForm`'s fields doesn't re-open a fresh form
  // out from under the one already open.
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'n') {
        event.preventDefault();
        openCreate();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, []);

  async function openEdit(summary: ConnectionSummary) {
    const full = await getConnection(summary.id);
    setFormState({ open: true, initialValues: full });
  }

  async function handleDuplicate(summary: ConnectionSummary) {
    const full = await getConnection(summary.id);
    await useConnectionStore.getState().saveConnection({
      ...full,
      id: 0,
      name: t('connections.screen.duplicateSuffix', { name: full.name }),
    });
  }

  async function handleDelete(summary: ConnectionSummary) {
    const pendingCount = useTransferStore
      .getState()
      .queue.filter((task) => task.profileId === summary.id).length;

    const confirmMessage =
      pendingCount > 0
        ? t('connections.screen.deleteConfirmWithTransfers', { name: summary.name, count: pendingCount })
        : t('connections.screen.deleteConfirm', { name: summary.name });

    const confirmed = await confirmDialog(confirmMessage, {
      danger: true,
      confirmLabel: t('common.delete'),
    });
    if (!confirmed) return;
    if (pendingCount > 0) {
      await cancelTasksForProfile(summary.id);
    }
    await deleteConnection(summary.id);
    // Deleting the profile the File Manager session is currently pointed at
    // must drop that session too — otherwise the Sidebar's active-connection
    // indicator (Block L2) keeps referencing a now-nonexistent profile, and
    // clicking it re-enters a File Manager for a connection that's gone
    // (Stage 4 Block L5).
    if (useFileManagerStore.getState().activeProfileId === summary.id) {
      useFileManagerStore.getState().reset();
    }
    // The backend cascade-deletes this profile's favorites at the DB level
    // (ON DELETE CASCADE, Block C) the instant deleteConnection resolves,
    // but useFavoritesStore's in-memory list was only ever populated once
    // at app boot — without an explicit refresh here, the Sidebar's
    // favorites section keeps showing the now-orphaned-in-the-UI (though
    // already gone in the DB) entries until the app is restarted.
    await useFavoritesStore.getState().fetchFavorites();
  }

  async function handleExport() {
    setIsExporting(true);
    try {
      // No success toast: the native save dialog itself is the user's
      // confirmation (same reasoning as `PresignedUrlModal`'s clipboard
      // copy — the action's own UI is feedback enough).
      await exportProfiles();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : t('connections.screen.exportError'));
    } finally {
      setIsExporting(false);
    }
  }

  async function handleImport() {
    setIsImporting(true);
    try {
      const result = await importProfiles();
      if (result.importedCount > 0 || result.skippedNames.length > 0) {
        toast.success(
          t('connections.screen.importResult', {
            imported: result.importedCount,
            skipped: result.skippedNames.length,
          }),
        );
        await useConnectionStore.getState().fetchConnections();
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : t('connections.screen.importError'));
    } finally {
      setIsImporting(false);
    }
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar
        activeItem="connections"
        onSelectTransfers={onSelectTransfers}
        onSelectHistory={onSelectHistory}
        onSelectSettings={onSelectSettings}
        onSelectFileManager={onSelectFileManager}
        onDisconnect={onDisconnect}
        onSelectFavorite={onSelectFavorite}
      />

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-4">
          <h1 className="text-[13px] font-semibold text-fg-primary">{t('connections.screen.title')}</h1>
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              disabled={connections.length === 0}
              isLoading={isExporting}
              onClick={handleExport}
            >
              <Download className="h-3.5 w-3.5" aria-hidden="true" />
              {t('connections.screen.exportButton')}
            </Button>
            <Button variant="secondary" isLoading={isImporting} onClick={handleImport}>
              <Upload className="h-3.5 w-3.5" aria-hidden="true" />
              {t('connections.screen.importButton')}
            </Button>
          </div>
        </header>

        <main className="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
          <ConnectionList
            connections={connections}
            isLoading={isLoading}
            onAdd={openCreate}
            onConnect={onConnect}
            onEdit={openEdit}
            onDuplicate={handleDuplicate}
            onDelete={handleDelete}
            onTest={openEdit}
          />

          {connections.length > 0 && (
            <div className="flex justify-center pt-2">
              <Button variant="secondary" onClick={openCreate}>
                {t('connections.screen.addButton')}
              </Button>
            </div>
          )}
        </main>
      </div>

      <ConnectionForm
        isOpen={formState.open}
        onClose={() => setFormState({ open: false })}
        initialValues={formState.open ? formState.initialValues : undefined}
        onSaved={() => {}}
      />
    </div>
  );
}
