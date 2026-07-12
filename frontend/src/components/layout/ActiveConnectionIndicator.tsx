import { Plug } from 'lucide-react';
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
export function ActiveConnectionIndicator({ profileName, isActive, onClick }: ActiveConnectionIndicatorProps) {
  const { t } = useTranslation();
  const tooltip = isActive
    ? t('sidebar.activeConnectionCurrentTooltip', { name: profileName })
    : t('sidebar.activeConnectionTooltip', { name: profileName });

  return (
    <div className="px-4 py-2">
      <Tooltip content={tooltip}>
        <button
          type="button"
          onClick={onClick}
          className={cn(
            'flex w-full items-center gap-2 rounded-sm px-2.5 py-2 text-left text-xs transition-colors duration-fast',
            isActive
              ? 'bg-accent-subtle text-accent'
              : 'bg-bg-tertiary text-fg-secondary hover:bg-bg-tertiary/80',
          )}
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
    </div>
  );
}
