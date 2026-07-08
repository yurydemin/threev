import { ArrowLeftRight, Cloud, History, Settings } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/utils';

interface NavItem {
  label: string;
  icon: LucideIcon;
  active?: boolean;
  disabled?: boolean;
}

// Only "Подключения" is wired to a screen in Stage 1 (Foundation). The rest
// are rendered per constraint #12 of the Stage 1 plan — visible so the
// overall IA reads correctly, but inert placeholders for later stages.
const NAV_ITEMS: NavItem[] = [
  { label: 'Подключения', icon: Cloud, active: true },
  { label: 'Передачи', icon: ArrowLeftRight, disabled: true },
  { label: 'История', icon: History, disabled: true },
  { label: 'Настройки', icon: Settings, disabled: true },
];

const APP_VERSION = 'v0.1.0';

/**
 * Left navigation panel per docs/03-ux-ui-spec.md section 5.2 ("Sidebar").
 */
export function Sidebar() {
  return (
    <aside className="flex h-full w-sidebar shrink-0 flex-col bg-bg-primary">
      <div className="flex items-center gap-2.5 p-4">
        <Cloud className="h-6 w-6 text-accent" aria-hidden="true" />
        <span className="text-[13px] font-semibold text-fg-primary">S3 Client</span>
      </div>

      <div className="border-t border-border" />

      <nav className="flex flex-col py-2">
        {NAV_ITEMS.map(({ label, icon: Icon, active, disabled }) => (
          <button
            key={label}
            type="button"
            disabled={disabled}
            className={cn(
              'flex h-row items-center gap-2.5 px-4 text-[13px] transition-colors duration-fast',
              active
                ? 'bg-accent-subtle text-accent'
                : disabled
                  ? 'cursor-not-allowed text-fg-secondary opacity-50'
                  : 'text-fg-secondary hover:bg-bg-tertiary',
            )}
          >
            <Icon className="h-4 w-4 shrink-0" aria-hidden="true" />
            {label}
          </button>
        ))}
      </nav>

      <div className="mt-auto p-4 text-2xs text-fg-muted">{APP_VERSION}</div>
    </aside>
  );
}
