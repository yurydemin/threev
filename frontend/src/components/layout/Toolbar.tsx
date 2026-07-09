import { ChevronLeft, ChevronRight, LayoutGrid, List, RotateCcw, Search } from 'lucide-react';
import { cn } from '../../lib/utils';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { Button } from '../ui/Button';
import { Breadcrumbs } from '../file-manager/Breadcrumbs';

export type FileManagerView = 'list' | 'grid';

export interface ToolbarProps {
  /**
   * List/grid preference. Kept as parent (`FileManagerScreen`) local state
   * rather than in `useFileManagerStore`: it is a pure display preference
   * with no bearing on navigation/data (unlike `sortBy`/`searchQuery`, it
   * never needs to survive a `reset()` back to Connections, nor does any
   * other component need to read it), so folding it into the session store
   * would only add noise. If a future stage wants it to persist across
   * sessions, `useAppStore` (already persisted, e.g. `sidebarCollapsed`)
   * is the more natural home than the file-manager session store.
   */
  view: FileManagerView;
  onViewChange: (view: FileManagerView) => void;
}

/**
 * Toolbar per docs/03-ux-ui-spec.md section 5.4.1.
 *
 * Reads navigation state (`history`, `selectedBucket`, `currentPrefix`,
 * `searchQuery`) directly from `useFileManagerStore` — like `BucketPanel`,
 * it has no meaningful behavior independent of that store.
 */
export function Toolbar({ view, onViewChange }: ToolbarProps) {
  const history = useFileManagerStore((state) => state.history);
  const historyIndex = useFileManagerStore((state) => state.historyIndex);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const searchQuery = useFileManagerStore((state) => state.searchQuery);
  const goBack = useFileManagerStore((state) => state.goBack);
  const goForward = useFileManagerStore((state) => state.goForward);
  const refresh = useFileManagerStore((state) => state.refresh);
  const navigateToPrefix = useFileManagerStore((state) => state.navigateToPrefix);
  const setSearchQuery = useFileManagerStore((state) => state.setSearchQuery);

  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < history.length - 1;

  return (
    <div className="flex h-header shrink-0 items-center justify-between gap-4 border-b border-border bg-bg-secondary px-4">
      <div className="flex min-w-0 items-center gap-2">
        <Button
          iconOnly
          variant="ghost"
          disabled={!canGoBack}
          onClick={() => goBack()}
          aria-label="Назад"
        >
          <ChevronLeft className="h-5 w-5" aria-hidden="true" />
        </Button>
        <Button
          iconOnly
          variant="ghost"
          disabled={!canGoForward}
          onClick={() => goForward()}
          aria-label="Вперёд"
        >
          <ChevronRight className="h-5 w-5" aria-hidden="true" />
        </Button>
        <Button iconOnly variant="ghost" onClick={() => refresh()} aria-label="Обновить">
          <RotateCcw className="h-5 w-5" aria-hidden="true" />
        </Button>

        <div className="mx-1 h-6 w-px shrink-0 bg-border" aria-hidden="true" />

        {selectedBucket && (
          <Breadcrumbs bucket={selectedBucket} prefix={currentPrefix} onNavigate={navigateToPrefix} />
        )}
      </div>

      <div className="flex shrink-0 items-center gap-2">
        <div className="relative">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-muted"
            aria-hidden="true"
          />
          <input
            type="search"
            value={searchQuery}
            onChange={(event) => setSearchQuery(event.target.value)}
            placeholder="Поиск в текущей папке…"
            className={cn(
              'h-8 rounded border border-border bg-bg-secondary pl-8 pr-2.5 text-[13px] text-fg-primary',
              'placeholder:text-fg-muted transition-[width,border-color] duration-fast',
              'focus:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-subtle',
              'w-[200px] focus:w-[280px]',
            )}
          />
        </div>

        <div className="flex items-center gap-0.5 rounded-sm border border-border p-0.5">
          <Button
            iconOnly
            variant={view === 'list' ? 'secondary' : 'ghost'}
            className={cn('h-7 w-7', view === 'list' && 'border-none bg-bg-tertiary')}
            onClick={() => onViewChange('list')}
            aria-label="Список"
            aria-pressed={view === 'list'}
          >
            <List className="h-4 w-4" aria-hidden="true" />
          </Button>
          <Button
            iconOnly
            variant={view === 'grid' ? 'secondary' : 'ghost'}
            className={cn('h-7 w-7', view === 'grid' && 'border-none bg-bg-tertiary')}
            onClick={() => onViewChange('grid')}
            aria-label="Сетка"
            aria-pressed={view === 'grid'}
          >
            <LayoutGrid className="h-4 w-4" aria-hidden="true" />
          </Button>
        </div>
      </div>
    </div>
  );
}
