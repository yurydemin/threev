import { useState } from 'react';
import { Sidebar } from '../components/layout/Sidebar';
import { Toolbar, type FileManagerView } from '../components/layout/Toolbar';
import { StatusBar } from '../components/layout/StatusBar';
import { BucketPanel } from '../components/file-manager/BucketPanel';
import { useFileManagerStore } from '../stores/useFileManagerStore';

export interface FileManagerScreenProps {
  profileId: number;
  profileName: string;
  /** Returns to the Connections screen (also resets `useFileManagerStore`). */
  onExit: () => void;
}

/**
 * File Manager screen — layout shell (Stage 2, Block G), per Architectural
 * Decision 6 of the Stage 2 plan.
 *
 * `<main>`'s content is still a placeholder — the actual Object List
 * (table/grid, sorting, skeleton/empty states, "Load more") is Block H, the
 * next step. Everything around it (main nav, `BucketPanel`, `Toolbar` with
 * back/forward/refresh/breadcrumbs/search/view-switch, `StatusBar`) is
 * fully wired to `useFileManagerStore` here.
 */
export function FileManagerScreen({ profileId, profileName, onExit }: FileManagerScreenProps) {
  const [view, setView] = useState<FileManagerView>('list');
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const entries = useFileManagerStore((state) => state.entries);

  return (
    <div className="flex h-screen w-full">
      <Sidebar onSelectConnections={onExit} />
      <BucketPanel />

      <div className="flex min-w-0 flex-1 flex-col">
        <Toolbar view={view} onViewChange={setView} />

        <main className="flex flex-1 items-center justify-center overflow-auto">
          {selectedBucket ? (
            <p className="text-sm text-fg-muted">Object List — Блок H</p>
          ) : (
            <p className="text-sm text-fg-muted">
              Выберите бакет слева, чтобы просмотреть его содержимое
            </p>
          )}
        </main>

        <StatusBar left={selectedBucket ? `${entries.length} объектов` : undefined} />
      </div>
    </div>
  );
}
