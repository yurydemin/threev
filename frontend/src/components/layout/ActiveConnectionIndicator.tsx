import { Plug, X } from 'lucide-react';
import type { MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { Tooltip } from '../ui/Tooltip';

export interface ActiveConnectionIndicatorProps {
  /** Name of the currently open File Manager session's profile (`Sidebar` only renders this component once it's non-null). */
  profileName: string;
  /**
   * Whether the user is currently viewing this session's File Manager
   * screen (Stage 4 Block L4) — `Sidebar` computes this from its own
   * `resolvedActiveItem === 'fileManager'`. Styled like an already-selected
   * nav item (`bg-accent-subtle text-accent`, same language as the active
   * "Подключения"/"Передачи"/"Настройки" buttons above it) so it reads as
   * "you are here", instead of the neutral clickable chip shown on every
   * other screen.
   */
  isActive: boolean;
  /** Returns to the open File Manager session. */
  onClick?: () => void;
  /**
   * Closes the open File Manager session (resets `useFileManagerStore` and
   * returns to Connections). Rendered as a small, independent "X" icon
   * button next to the primary chip — not nested inside it, since a
   * `<button>` inside another `<button>` is invalid HTML — so it needs its
   * own click handler to stop the click from also bubbling into the
   * primary button's `onClick`. Optional for the same reason as `onClick`
   * (matches this codebase's optional-callback convention, e.g.
   * `SidebarProps`'s `onSelect*` props).
   */
  onDisconnect?: () => void;
}

/**
 * `Sidebar` indicator (Stage 4 Block L2, restyled Block L4) for "there's a
 * File Manager session still open" — surfaces on every screen, including
 * the File Manager itself (previously hidden there via
 * `hideActiveConnectionIndicator`, removed in Block L4 once the File
 * Manager gained its own `activeItem: 'fileManager'` and none of the static
 * nav items highlighted while browsing — see `Sidebar`'s doc-comment).
 *
 * Uses `Plug` rather than `Cloud` (already the app logo AND the
 * "Подключения" nav icon right above this in the sidebar) to avoid visually
 * implying this is a third, unrelated "connections" affordance.
 *
 * The small `bg-success` dot is a session-status cue ("this connection is
 * live"), shown regardless of `isActive` — it answers a different question
 * ("is there an open session at all?") than the active/inactive styling
 * does ("am I looking at it right now?").
 *
 * `profileName` is required (not `string | null`) — the presence check
 * lives in `Sidebar`, which only mounts this component once there's an
 * active profile to show, keeping this component's own props simple.
 */
export function ActiveConnectionIndicator({ profileName, isActive, onClick, onDisconnect }: ActiveConnectionIndicatorProps) {
  const { t } = useTranslation();
  const tooltip = isActive
    ? t('sidebar.activeConnectionCurrentTooltip', { name: profileName })
    : t('sidebar.activeConnectionTooltip', { name: profileName });

  function handleDisconnectClick(event: MouseEvent<HTMLButtonElement>) {
    // Stops the click from also reaching the primary chip's `onClick`
    // (which would re-enter the very session this button just closed) —
    // the two buttons are siblings, not nested, but they still overlap the
    // same small hit area visually.
    event.stopPropagation();
    onDisconnect?.();
  }

  return (
    <div className="px-4 py-2">
      <div
        className={cn(
          'flex items-center gap-2 rounded-sm px-2.5 py-2 transition-colors duration-fast',
          isActive
            ? 'bg-accent-subtle text-accent'
            : 'bg-bg-tertiary text-fg-secondary hover:bg-bg-tertiary/80',
        )}
      >
        <Tooltip content={tooltip}>
          <button
            type="button"
            onClick={onClick}
            className="flex min-w-0 flex-1 items-center gap-2 text-left text-xs"
          >
            <span className="relative flex shrink-0 items-center justify-center">
              <Plug className="h-3.5 w-3.5" aria-hidden="true" />
              <span
                className="absolute -bottom-0.5 -right-0.5 h-1.5 w-1.5 rounded-full bg-success"
                aria-hidden="true"
              />
            </span>
            <span className="truncate">{t('sidebar.activeConnection', { name: profileName })}</span>
          </button>
        </Tooltip>
        {onDisconnect && (
          <Tooltip content={t('sidebar.disconnectTooltip', { name: profileName })}>
            <button
              type="button"
              onClick={handleDisconnectClick}
              aria-label={t('sidebar.disconnect')}
              className="flex h-5 w-5 shrink-0 items-center justify-center rounded-sm text-fg-muted transition-colors duration-fast hover:bg-bg-tertiary hover:text-fg-primary"
            >
              <X className="h-3.5 w-3.5" aria-hidden="true" />
            </button>
          </Tooltip>
        )}
      </div>
    </div>
  );
}
