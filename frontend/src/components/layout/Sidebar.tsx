import { ArrowLeftRight, ChevronDown, Cloud, History, Settings, Star } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { useAppStore } from '../../stores/useAppStore';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { useFavoritesStore } from '../../stores/useFavoritesStore';
import type { Favorite } from '../../types';
import { ActiveConnectionIndicator } from './ActiveConnectionIndicator';
import { Tooltip } from '../ui/Tooltip';

interface NavItem {
  label: string;
  icon: LucideIcon;
  active?: boolean;
  disabled?: boolean;
  onClick?: () => void;
}

export type SidebarActiveItem = 'connections' | 'transfers' | 'history' | 'settings' | 'fileManager';

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
  /** Called when "История" is clicked. Same optionality rationale as `onSelectConnections`. */
  onSelectHistory?: () => void;
  /** Called when "Настройки" is clicked (Stage 4, Block G). Same optionality rationale as `onSelectConnections`. */
  onSelectSettings?: () => void;
  /**
   * Called when the active-connection indicator (Stage 4, Block L2) is
   * clicked, returning to the already-open File Manager session. Optional
   * for the same reason as `onSelectConnections`/etc.
   */
  onSelectFileManager?: () => void;
  /**
   * Called when the active-connection indicator's own "X" button is
   * clicked, closing the open File Manager session. Optional for the same
   * reason as `onSelectConnections`/etc.
   */
  onDisconnect?: () => void;
  /**
   * Called when a favorite row in the new Favorites section is clicked.
   * Optional for the same reason as `onSelectConnections`/etc. Navigation
   * itself (same-profile vs. switch-session-then-navigate) is decided by
   * `App.tsx`'s `handleSelectFavorite`, not here — this component only
   * surfaces the click.
   */
  onSelectFavorite?: (favorite: Favorite) => void;
}

/** Renders `bucket` alone if `prefix` is empty (bucket root), or `bucket/prefix` otherwise. No custom label field exists on `Favorite` — this is always computed directly from the two fields. */
function favoriteLabel(favorite: Favorite): string {
  return favorite.prefix ? `${favorite.bucket}/${favorite.prefix}` : favorite.bucket;
}

/**
 * Left navigation panel per docs/03-ux-ui-spec.md section 5.2 ("Sidebar").
 *
 * Per Stage 2 constraint #1, this panel stays visible (unchanged) across
 * every top-level screen, including the File Manager — it is not replaced
 * by the file-manager-specific sidebar from UX-spec section 5.4.2 (that
 * becomes the separate `BucketPanel`, Block G).
 *
 * "Подключения", "Передачи" (Stage 3 Block K), "История" and "Настройки"
 * (Stage 4 Block G) are all live navigation targets, highlighted via
 * `activeItem`.
 */
export function Sidebar({
  activeItem,
  onSelectConnections,
  onSelectTransfers,
  onSelectHistory,
  onSelectSettings,
  onSelectFileManager,
  onDisconnect,
  onSelectFavorite,
}: SidebarProps) {
  const { t } = useTranslation();
  const appVersion = useAppStore((state) => state.appVersion);
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const activeProfileName = useFileManagerStore((state) => state.activeProfileName);
  const favorites = useFavoritesStore((state) => state.favorites);
  const [isFavoritesExpanded, setIsFavoritesExpanded] = useState(true);
  const resolvedActiveItem = activeItem ?? 'connections';

  // Grouped by `profileName` (not a flat list) - several profiles can each
  // have their own favorites, and a flat list mixing bucket names from
  // different accounts would be confusing. Insertion order (a `Map`)
  // preserves whatever order `GetFavorites()` returned them in, rather than
  // re-sorting profile groups alphabetically.
  const favoritesByProfile = new Map<string, Favorite[]>();
  for (const favorite of favorites) {
    const group = favoritesByProfile.get(favorite.profileName);
    if (group) {
      group.push(favorite);
    } else {
      favoritesByProfile.set(favorite.profileName, [favorite]);
    }
  }
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
    {
      label: t('sidebar.history'),
      icon: History,
      active: resolvedActiveItem === 'history',
      onClick: onSelectHistory,
    },
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
        Favorites section - collapsible, expanded by default, hidden
        entirely when there are zero favorites (matches how
        `ActiveConnectionIndicator` only renders when there's something to
        show). Its own bounded `max-h-48` + `overflow-y-auto` keeps a long
        list scrolling internally rather than pushing the nav items above or
        the version-number footer (pinned via `mt-auto` on `<aside>`) out of
        place.
      */}
      {favorites.length > 0 && (
        <>
          <div className="border-t border-border" />
          <div className="flex flex-col py-2">
            <button
              type="button"
              onClick={() => setIsFavoritesExpanded((prev) => !prev)}
              aria-expanded={isFavoritesExpanded}
              aria-label={t('sidebar.favoritesToggleTooltip')}
              className="flex h-row shrink-0 items-center justify-between gap-2.5 px-4 text-[13px] text-fg-secondary transition-colors duration-fast hover:bg-bg-tertiary"
            >
              <span className="flex items-center gap-2.5">
                <Star className="h-4 w-4 shrink-0" aria-hidden="true" />
                {t('sidebar.favorites')}
              </span>
              <ChevronDown
                className={cn('h-3.5 w-3.5 shrink-0 transition-transform duration-fast', !isFavoritesExpanded && '-rotate-90')}
                aria-hidden="true"
              />
            </button>

            {isFavoritesExpanded && (
              <div className="max-h-48 overflow-y-auto">
                {Array.from(favoritesByProfile.entries()).map(([profileName, profileFavorites]) => (
                  <div key={profileName} className="flex flex-col">
                    <div className="px-4 pb-1 pt-1.5 text-2xs font-medium uppercase tracking-wide text-fg-muted">
                      {profileName}
                    </div>
                    {profileFavorites.map((favorite) => (
                      <Tooltip key={favorite.id} content={favoriteLabel(favorite)}>
                        <button
                          type="button"
                          onClick={() => onSelectFavorite?.(favorite)}
                          className="flex h-row min-w-0 items-center px-4 pl-9 text-left text-[13px] text-fg-secondary transition-colors duration-fast hover:bg-bg-tertiary"
                        >
                          <span className="truncate">{favoriteLabel(favorite)}</span>
                        </button>
                      </Tooltip>
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}

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
            onDisconnect={onDisconnect}
          />
        </>
      )}

      <div className="mt-auto p-4 text-2xs text-fg-muted">{appVersion}</div>
    </aside>
  );
}
