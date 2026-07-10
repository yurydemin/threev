import { useState } from 'react';
import { Sidebar } from '../components/layout/Sidebar';
import { StatusBar } from '../components/layout/StatusBar';
import { TransferTabs, type TransferTab } from '../components/transfer/TransferTabs';
import { GroupActions } from '../components/transfer/GroupActions';
import { TransferCard } from '../components/transfer/TransferCard';
import { HistoryCard } from '../components/transfer/HistoryCard';
import { EmptyState } from '../components/transfer/EmptyState';
import { useTransferStore } from '../stores/useTransferStore';

const EMPTY_MESSAGES: Record<TransferTab, string> = {
  active: 'Нет активных передач',
  completed: 'Нет завершённых передач',
  all: 'Нет передач',
};

/**
 * "Передачи" screen per docs/03-ux-ui-spec.md section 5.5.
 *
 * Doesn't call `fetchQueue`/`fetchHistory` itself - `useTransferEvents()`
 * (mounted once at the `App.tsx` level, Stage 3 Block K) already hydrates
 * and keeps `useTransferStore` up to date via "transfer:progress" events,
 * so this screen just reads the store.
 *
 * Tab-to-data mapping (see Block I task notes - `Failed` tasks are NOT
 * archived to `history` on the backend, they stay in `queue` so `RetryTask`
 * can act on them):
 *  - "Активные" = `queue` as-is (`pending`/`running`/`paused`/`failed` all
 *    mixed together - a failed task is still actionable, not tucked away).
 *  - "Завершённые" = `history` (`completed`/`cancelled` only).
 *  - "Все" = `queue` followed by `history` (history's own "most recent
 *    completion first" order is preserved, not re-sorted).
 */
export interface TransferScreenProps {
  /** Returns to the Connections screen (Sidebar "Подключения"). */
  onSelectConnections: () => void;
  /** Navigates to the Settings screen (Sidebar "Настройки"). */
  onSelectSettings: () => void;
}

export function TransferScreen({ onSelectConnections, onSelectSettings }: TransferScreenProps) {
  const [tab, setTab] = useState<TransferTab>('active');
  const queue = useTransferStore((state) => state.queue);
  const history = useTransferStore((state) => state.history);
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

  const showQueue = tab === 'active' || tab === 'all';
  const showHistory = tab === 'completed' || tab === 'all';
  const isEmpty = (!showQueue || queue.length === 0) && (!showHistory || history.length === 0);

  return (
    <div className="flex h-screen w-full">
      <Sidebar activeItem="transfers" onSelectConnections={onSelectConnections} onSelectSettings={onSelectSettings} />

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-4">
          <h1 className="text-[13px] font-semibold text-fg-primary">Передачи</h1>
        </header>

        <TransferTabs active={tab} onChange={setTab} activeCount={queue.length} />

        <main className="flex flex-1 flex-col overflow-y-auto p-4">
          {isEmpty ? (
            <EmptyState message={EMPTY_MESSAGES[tab]} />
          ) : (
            <div className="flex flex-col gap-3">
              {showQueue &&
                queue.map((task) => (
                  <TransferCard
                    key={task.id}
                    task={task}
                    onPause={pauseTask}
                    onResume={resumeTask}
                    onCancel={cancelTask}
                    onRetry={retryTask}
                  />
                ))}
              {showHistory && history.map((entry) => <HistoryCard key={entry.id} entry={entry} />)}
            </div>
          )}
        </main>

        {tab === 'active' && (
          <GroupActions
            queue={queue}
            onPauseAll={handlePauseAll}
            onResumeAll={handleResumeAll}
            onCancelAll={handleCancelAll}
          />
        )}

        <StatusBar />
      </div>
    </div>
  );
}
