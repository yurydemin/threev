import { ArrowLeftRight, Cloud, History, Settings } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { APP_VERSION } from '../../lib/appVersion';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { ActiveConnectionIndicator } from './ActiveConnectionIndicator';

interface NavItem {
  label: string;
  icon: LucideIcon;
  active?: boolean;
  disabled?: boolean;
  onClick?: () => void;
}

export type SidebarActiveItem = 'connections' | 'transfers' | 'settings' | 'fileManager';

export interface SidebarProps {
  /**
   * Which nav item is highlighted. Defaults to 'connections' — preserves
   * every existing call site (`ConnectionsScreen`) unchanged if it doesn't
   * pass it. `FileManagerScreen` always passes `'fileManager'` explicitly
   * (Stage 4 Block L4) — that value doesn't match any of the static nav
   * items below, so none of them highlight while browsing; the
   * active-connection indicator itself becomes the "you are here" cue
   * instead (see its own `isActive` prop).
   */
  activeItem?: SidebarActiveItem;
  /**
   * Called when "Подключения" is clicked. Optional so existing call sites
   * (e.g. `ConnectionsScreen`, where this is already the active screen)
   * don't have to pass a no-op — the item is still keyboard/mouse
   * interactive there, it just has nowhere further to navigate to.
   */
  onSelectConnections?: () => void;
  /** Called when "Передачи" is clicked. Same optionality rationale as `onSelectConnections`. */
  onSelectTransfers?: () => void;
  /** Called when "Настройки" is clicked (Stage 4, Block G). Same optionality rationale as `onSelectConnections`. */
  onSelectSettings?: () => void;
  /**
   * Called when the active-connection indicator (Stage 4, Block L2) is
   * clicked, returning to the already-open File Manager session. Optional
   * for the same reason as `onSelectConnections`/etc.
   */
  onSelectFileManager?: () => void;
}

/**
 * Left navigation panel per docs/03-ux-ui-spec.md section 5.2 ("Sidebar").
 *
 * Per Stage 2 constraint #1, this panel stays visible (unchanged) across
 * every top-level screen, including the File Manager — it is not replaced
 * by the file-manager-specific sidebar from UX-spec section 5.4.2 (that
 * becomes the separate `BucketPanel`, Block G).
 *
 * "Подключения", "Передачи" (Stage 3 Block K) and "Настройки" (Stage 4
 * Block G) are all live navigation targets, highlighted via `activeItem`.
 * "История" remains an inert placeholder (Stage 1 plan constraint #12)
 * until its own stage lands.
 */
export function Sidebar({
  activeItem,
  onSelectConnections,
  onSelectTransfers,
  onSelectSettings,
  onSelectFileManager,
}: SidebarProps) {
  const { t } = useTranslation();
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const activeProfileName = useFileManagerStore((state) => state.activeProfileName);
  const resolvedActiveItem = activeItem ?? 'connections';
  const navItems: NavItem[] = [
    {
      label: t('sidebar.connections'),
      icon: Cloud,
      active: resolvedActiveItem === 'connections',
      onClick: onSelectConnections,
    },
    {
      label: t('sidebar.transfers'),
      icon: ArrowLeftRight,
      active: resolvedActiveItem === 'transfers',
      onClick: onSelectTransfers,
    },
    { label: t('sidebar.history'), icon: History, disabled: true },
    {
      label: t('sidebar.settings'),
      icon: Settings,
      active: resolvedActiveItem === 'settings',
      onClick: onSelectSettings,
    },
  ];

  return (
    <aside className="flex h-full w-sidebar shrink-0 flex-col bg-bg-primary">
      <div className="flex items-center gap-2.5 p-4">
        <Cloud className="h-6 w-6 text-accent" aria-hidden="true" />
        <span className="text-[13px] font-semibold text-fg-primary">{t('sidebar.appName')}</span>
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

      {/*
        Placed directly below `nav` (not down by the version footer) so it
        reads as "where the active session lives, relative to navigation" —
        a one-click way back into it — rather than as incidental metadata
        tucked away at the bottom of the panel.
      */}
      {activeProfileId !== null && activeProfileName !== null && (
        <>
          <div className="border-t border-border" />
          <ActiveConnectionIndicator
            profileName={activeProfileName}
            isActive={resolvedActiveItem === 'fileManager'}
            onClick={onSelectFileManager}
          />
        </>
      )}

      <div className="mt-auto p-4 text-2xs text-fg-muted">{APP_VERSION}</div>
    </aside>
  );
}
