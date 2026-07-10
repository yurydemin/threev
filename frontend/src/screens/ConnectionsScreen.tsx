import { useState } from 'react';
import { Sidebar } from '../components/layout/Sidebar';
import { ConnectionForm } from '../components/connection/ConnectionForm';
import { ConnectionList } from '../components/connection/ConnectionList';
import { Button } from '../components/ui/Button';
import { getConnection } from '../lib/wails/connection';
import { useConnectionStore } from '../stores/useConnectionStore';
import type { Connection, ConnectionSummary } from '../types';

type FormState = { open: false } | { open: true; initialValues?: Connection };

export interface ConnectionsScreenProps {
  /** Enters the File Manager for this connection ("Подключиться" on a card). */
  onConnect: (connection: ConnectionSummary) => void;
  /** Navigates to the Transfers screen (Sidebar "Передачи"). */
  onSelectTransfers: () => void;
}

/**
 * "Список подключений" per docs/03-ux-ui-spec.md section 5.2.
 *
 * The spec's "Import" button next to "+ Новое" is deferred (Stage 1 plan
 * constraint #12) and intentionally not rendered here.
 *
 * "Дублировать" and delete are instant actions (no intermediate modal):
 * duplicate re-saves the fetched record with `id: 0` (create-new, per
 * `SaveProfile` semantics) and a "(копия)" suffix; delete uses a native
 * `window.confirm` since no generic confirmation-dialog component exists
 * yet in this build.
 *
 * The card menu's "Тестировать" opens the same edit modal as
 * "Редактировать" (rather than testing silently in the background) — there
 * is no toast/notification system yet to surface a background test result,
 * so routing through the form's own test UI is the honest choice.
 */
export function ConnectionsScreen({ onConnect, onSelectTransfers }: ConnectionsScreenProps) {
  const connections = useConnectionStore((state) => state.connections);
  const isLoading = useConnectionStore((state) => state.isLoading);
  const deleteConnection = useConnectionStore((state) => state.deleteConnection);

  const [formState, setFormState] = useState<FormState>({ open: false });

  function openCreate() {
    setFormState({ open: true, initialValues: undefined });
  }

  async function openEdit(summary: ConnectionSummary) {
    const full = await getConnection(summary.id);
    setFormState({ open: true, initialValues: full });
  }

  async function handleDuplicate(summary: ConnectionSummary) {
    const full = await getConnection(summary.id);
    await useConnectionStore.getState().saveConnection({
      ...full,
      id: 0,
      name: `${full.name} (копия)`,
    });
  }

  async function handleDelete(summary: ConnectionSummary) {
    if (!window.confirm(`Удалить подключение «${summary.name}»?`)) return;
    await deleteConnection(summary.id);
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar activeItem="connections" onSelectTransfers={onSelectTransfers} />

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-4">
          <h1 className="text-[13px] font-semibold text-fg-primary">Подключения</h1>
          <Button variant="primary" onClick={openCreate}>
            + Новое
          </Button>
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
                + Добавить подключение
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
