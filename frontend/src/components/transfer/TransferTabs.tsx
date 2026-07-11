import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { cn } from '../../lib/utils';

export type TransferTab = 'active' | 'completed' | 'all';

export interface TransferTabsProps {
  active: TransferTab;
  onChange: (tab: TransferTab) => void;
  /** `queue.length`, shown in parentheses next to "Активные" only (UX spec 5.5 mockup). */
  activeCount: number;
}

function getTabs(t: TFunction): Array<{ id: TransferTab; label: string }> {
  return [
    { id: 'active', label: t('transfers.tabs.active') },
    { id: 'completed', label: t('transfers.tabs.completed') },
    { id: 'all', label: t('transfers.tabs.all') },
  ];
}

/**
 * Tab strip for the Transfer screen, per docs/03-ux-ui-spec.md section 5.5.
 * Active tab gets a 2px accent underline + accent text; inactive tabs get a
 * `bg-bg-tertiary` hover highlight.
 */
export function TransferTabs({ active, onChange, activeCount }: TransferTabsProps) {
  const { t } = useTranslation();
  const TABS = getTabs(t);
  return (
    <div className="flex h-9 shrink-0 items-center gap-1 border-b border-border px-4" role="tablist">
      {TABS.map((tab) => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          aria-selected={active === tab.id}
          onClick={() => onChange(tab.id)}
          className={cn(
            'flex h-full items-center border-b-2 px-3 text-[13px] font-medium transition-colors duration-fast',
            active === tab.id
              ? 'border-accent text-accent'
              : 'border-transparent text-fg-secondary hover:bg-bg-tertiary',
          )}
        >
          {tab.label}
          {tab.id === 'active' && <span className="ml-1">({activeCount})</span>}
        </button>
      ))}
    </div>
  );
}
