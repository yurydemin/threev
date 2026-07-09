import { ArrowLeftRight, Cloud, History, Settings } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/utils';

interface NavItem {
  label: string;
  icon: LucideIcon;
  active?: boolean;
  disabled?: boolean;
  onClick?: () => void;
}

const APP_VERSION = 'v0.1.0';

export interface SidebarProps {
  /**
   * Called when "Подключения" is clicked. Optional so existing call sites
   * (e.g. `ConnectionsScreen`, where this is already the active screen)
   * don't have to pass a no-op — the item is still keyboard/mouse
   * interactive there, it just has nowhere further to navigate to.
   */
  onSelectConnections?: () => void;
}

/**
 * Left navigation panel per docs/03-ux-ui-spec.md section 5.2 ("Sidebar").
 *
 * Per Stage 2 constraint #1, this panel stays visible (unchanged) across
 * every top-level screen, including the File Manager — it is not replaced
 * by the file-manager-specific sidebar from UX-spec section 5.4.2 (that
 * becomes the separate `BucketPanel`, Block G).
 *
 * "Передачи"/"История"/"Настройки" remain inert placeholders (Stage 1 plan
 * constraint #12) until their respective stages land.
 */
export function Sidebar({ onSelectConnections }: SidebarProps) {
  const navItems: NavItem[] = [
    { label: 'Подключения', icon: Cloud, active: true, onClick: onSelectConnections },
    { label: 'Передачи', icon: ArrowLeftRight, disabled: true },
    { label: 'История', icon: History, disabled: true },
    { label: 'Настройки', icon: Settings, disabled: true },
  ];

  return (
    <aside className="flex h-full w-sidebar shrink-0 flex-col bg-bg-primary">
      <div className="flex items-center gap-2.5 p-4">
        <Cloud className="h-6 w-6 text-accent" aria-hidden="true" />
        <span className="text-[13px] font-semibold text-fg-primary">S3 Client</span>
      </div>

      <div className="border-t border-border" />

      <nav className="flex flex-col py-2">
        {navItems.map(({ label, icon: Icon, active, disabled, onClick }) => (
          <button
            key={label}
            type="button"
            disabled={disabled}
            onClick={onClick}
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
