import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Sidebar } from '../components/layout/Sidebar';
import { StatusBar } from '../components/layout/StatusBar';
import { Button } from '../components/ui/Button';
import { HistoryCard } from '../components/transfer/HistoryCard';
import { EmptyState } from '../components/transfer/EmptyState';
import { confirmDialog } from '../lib/confirm';
import { useTransferStore } from '../stores/useTransferStore';

/** Passed to `fetchHistory` on mount - larger than the store's own
 * `DEFAULT_HISTORY_LIMIT` (100), since this screen now owns history
 * exclusively and has the full main content area to show it in, unlike the
 * old inline "Завершённые" tab it replaces. */
const HISTORY_FETCH_LIMIT = 200;

export interface HistoryScreenProps {
  /** Returns to the Connections screen (Sidebar "Подключения"). */
  onSelectConnections: () => void;
  /** Navigates to the Transfers screen (Sidebar "Передачи"). */
  onSelectTransfers: () => void;
  /** Navigates to the Settings screen (Sidebar "Настройки"). */
  onSelectSettings: () => void;
  /** Returns to an already-open File Manager session (Sidebar active-connection indicator, Block L2). */
  onSelectFileManager: () => void;
}

/**
 * "История" screen - a top-level Sidebar destination for completed/cancelled
 * transfers, promoted out of the Transfer screen's old "Завершённые"/"Все"
 * tabs (`TransferTabs`, now removed) into its own screen.
 *
 * Unlike `TransferScreen` (which relies entirely on `useTransferEvents()`,
 * mounted once at the `App.tsx` root, to keep `queue`/`history` fresh),
 * this screen calls `fetchHistory(HISTORY_FETCH_LIMIT)` itself on mount:
 * `useTransferEvents` only refetches `history` with the store's own default
 * limit (100), which isn't enough now that this screen is the single place
 * to review transfer history and has the room to show more.
 */
export function HistoryScreen({
  onSelectConnections,
  onSelectTransfers,
  onSelectSettings,
  onSelectFileManager,
}: HistoryScreenProps) {
  const { t } = useTranslation();
  const history = useTransferStore((state) => state.history);
  const fetchHistory = useTransferStore((state) => state.fetchHistory);
  const clearHistory = useTransferStore((state) => state.clearHistory);
  const [isClearing, setIsClearing] = useState(false);

  useEffect(() => {
    void fetchHistory(HISTORY_FETCH_LIMIT);
  }, [fetchHistory]);

  async function handleClearHistory() {
    const confirmed = await confirmDialog(t('history.screen.clearConfirmMessage'), {
      title: t('history.screen.clearConfirmTitle'),
      confirmLabel: t('common.delete'),
      danger: true,
    });
    if (!confirmed) return;
    setIsClearing(true);
    await clearHistory();
    setIsClearing(false);
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar
        activeItem="history"
        onSelectConnections={onSelectConnections}
        onSelectTransfers={onSelectTransfers}
        onSelectSettings={onSelectSettings}
        onSelectFileManager={onSelectFileManager}
      />

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-4">
          <h1 className="text-[13px] font-semibold text-fg-primary">{t('history.screen.title')}</h1>
          <Button
            variant="secondary"
            disabled={history.length === 0}
            isLoading={isClearing}
            onClick={handleClearHistory}
          >
            {t('history.screen.clearButton')}
          </Button>
        </header>

        <main className="flex flex-1 flex-col overflow-y-auto p-4">
          {history.length === 0 ? (
            <EmptyState message={t('history.screen.empty')} />
          ) : (
            <div className="flex flex-col gap-3">
              {history.map((entry) => (
                <HistoryCard key={entry.id} entry={entry} />
              ))}
            </div>
          )}
        </main>

        <StatusBar />
      </div>
    </div>
  );
}
