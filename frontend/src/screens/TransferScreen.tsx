import { useTranslation } from 'react-i18next';
import { Sidebar } from '../components/layout/Sidebar';
import { StatusBar } from '../components/layout/StatusBar';
import { GroupActions } from '../components/transfer/GroupActions';
import { TransferCard } from '../components/transfer/TransferCard';
import { EmptyState } from '../components/transfer/EmptyState';
import { useTransferStore } from '../stores/useTransferStore';
import type { Favorite } from '../types';

/**
 * "Передачи" screen per docs/03-ux-ui-spec.md section 5.5.
 *
 * Doesn't call `fetchQueue` itself - `useTransferEvents()` (mounted once at
 * the `App.tsx` level, Stage 3 Block K) already hydrates and keeps
 * `useTransferStore` up to date via "transfer:progress" events, so this
 * screen just reads the store.
 *
 * Shows `queue` as-is (`pending`/`running`/`paused`/`failed` all mixed
 * together - a failed task is still actionable, not tucked away). Completed
 * / cancelled transfers no longer live here: they were promoted to their own
 * top-level "История" screen (`HistoryScreen`), which owns `history`
 * exclusively - see that screen for the "Завершённые"/"Все" views this
 * screen used to render via `TransferTabs`.
 */
export interface TransferScreenProps {
  /** Returns to the Connections screen (Sidebar "Подключения"). */
  onSelectConnections: () => void;
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

export function TransferScreen({
  onSelectConnections,
  onSelectHistory,
  onSelectSettings,
  onSelectFileManager,
  onDisconnect,
  onSelectFavorite,
}: TransferScreenProps) {
  const { t } = useTranslation();
  const queue = useTransferStore((state) => state.queue);
  const pauseTask = useTransferStore((state) => state.pauseTask);
  const resumeTask = useTransferStore((state) => state.resumeTask);
  const cancelTask = useTransferStore((state) => state.cancelTask);
  const retryTask = useTransferStore((state) => state.retryTask);

  async function handlePauseAll() {
    const ids = queue.filter((task) => task.status === 'pending' || task.status === 'running').map((task) => task.id);
    await Promise.all(ids.map((id) => pauseTask(id)));
  }

  async function handleResumeAll() {
    const ids = queue.filter((task) => task.status === 'paused').map((task) => task.id);
    await Promise.all(ids.map((id) => resumeTask(id)));
  }

  async function handleCancelAll() {
    const ids = queue.map((task) => task.id);
    await Promise.all(ids.map((id) => cancelTask(id)));
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar
        activeItem="transfers"
        onSelectConnections={onSelectConnections}
        onSelectHistory={onSelectHistory}
        onSelectSettings={onSelectSettings}
        onSelectFileManager={onSelectFileManager}
        onDisconnect={onDisconnect}
        onSelectFavorite={onSelectFavorite}
      />

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-4">
          <h1 className="text-[13px] font-semibold text-fg-primary">{t('transfers.screen.title')}</h1>
        </header>

        <main className="flex flex-1 flex-col overflow-y-auto p-4">
          {queue.length === 0 ? (
            <EmptyState message={t('transfers.screen.emptyActive')} />
          ) : (
            <div className="flex flex-col gap-3">
              {queue.map((task) => (
                <TransferCard
                  key={task.id}
                  task={task}
                  onPause={pauseTask}
                  onResume={resumeTask}
                  onCancel={cancelTask}
                  onRetry={retryTask}
                />
              ))}
            </div>
          )}
        </main>

        <GroupActions
          queue={queue}
          onPauseAll={handlePauseAll}
          onResumeAll={handleResumeAll}
          onCancelAll={handleCancelAll}
        />

        <StatusBar />
      </div>
    </div>
  );
}
