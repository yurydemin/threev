import { Sidebar } from '../components/layout/Sidebar';
import { Button } from '../components/ui/Button';

export interface FileManagerScreenProps {
  profileId: number;
  profileName: string;
  /** Returns to the Connections screen (also resets `useFileManagerStore`). */
  onExit: () => void;
}

/**
 * File Manager screen — MINIMAL placeholder (Stage 2, Block F).
 *
 * This only proves the navigation wiring end-to-end (clicking "Подключиться"
 * on a connection card lands here with the right profile, and both the main
 * nav's "Подключения" item and the local "Назад" button return to the
 * Connections screen). The real layout — `BucketPanel` + Toolbar +
 * Breadcrumbs + ObjectList, per Architectural Decision 6 of the Stage 2 plan
 * — is built in Block G and will replace this file's body entirely.
 */
export function FileManagerScreen({ profileId, profileName, onExit }: FileManagerScreenProps) {
  return (
    <div className="flex h-screen w-full">
      <Sidebar onSelectConnections={onExit} />

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-4">
          <h1 className="text-[13px] font-semibold text-fg-primary">Файловый менеджер</h1>
          <Button variant="secondary" onClick={onExit}>
            Назад к подключениям
          </Button>
        </header>

        <main className="flex flex-1 items-center justify-center p-4">
          <p className="text-sm text-fg-secondary">
            Профиль: {profileName} (ID: {profileId})
          </p>
        </main>
      </div>
    </div>
  );
}
