import { Cloud } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '../ui/Button';
import { ConnectionCard } from './ConnectionCard';
import type { ConnectionSummary } from '../../types';

export interface ConnectionListProps {
  connections: ConnectionSummary[];
  isLoading: boolean;
  onAdd: () => void;
  onConnect: (connection: ConnectionSummary) => void;
  onEdit: (connection: ConnectionSummary) => void;
  onDuplicate: (connection: ConnectionSummary) => void;
  onDelete: (connection: ConnectionSummary) => void;
  onTest: (connection: ConnectionSummary) => void;
}

const GRID_CLASSES = 'grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(300px,1fr))]';

function CardSkeleton() {
  return (
    <div className="flex animate-pulse flex-col gap-3 rounded border border-border bg-bg-secondary p-4">
      <div className="flex items-center gap-2">
        <div className="h-2 w-2 rounded-full bg-bg-tertiary" />
        <div className="h-3.5 w-32 rounded bg-bg-tertiary" />
      </div>
      <div className="h-3 w-full max-w-[220px] rounded bg-bg-tertiary" />
    </div>
  );
}

/**
 * Connection grid per docs/03-ux-ui-spec.md section 5.2 ("Карточка
 * подключения", grid `repeat(auto-fill, minmax(300px, 1fr))`, gap 12px) plus
 * loading/empty states per sections 9.1/9.2.
 */
export function ConnectionList({
  connections,
  isLoading,
  onAdd,
  onConnect,
  onEdit,
  onDuplicate,
  onDelete,
  onTest,
}: ConnectionListProps) {
  const { t } = useTranslation();
  if (isLoading && connections.length === 0) {
    return (
      <div className={GRID_CLASSES}>
        {Array.from({ length: 5 }).map((_, index) => (
          <CardSkeleton key={index} />
        ))}
      </div>
    );
  }

  if (connections.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 py-16 text-center">
        <Cloud className="h-12 w-12 text-fg-muted" aria-hidden="true" />
        <p className="text-sm text-fg-secondary">{t('connections.list.empty')}</p>
        <Button variant="primary" onClick={onAdd}>
          {t('connections.list.add')}
        </Button>
      </div>
    );
  }

  return (
    <div className={GRID_CLASSES}>
      {connections.map((connection) => (
        <ConnectionCard
          key={connection.id}
          connection={connection}
          onConnect={onConnect}
          onEdit={onEdit}
          onDuplicate={onDuplicate}
          onDelete={onDelete}
          onTest={onTest}
        />
      ))}
    </div>
  );
}
