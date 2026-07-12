import { Plug } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Tooltip } from '../ui/Tooltip';

export interface ActiveConnectionIndicatorProps {
  /** Name of the currently open File Manager session's profile (`Sidebar` only renders this component once it's non-null). */
  profileName: string;
  /** Returns to the open File Manager session. */
  onClick?: () => void;
}

/**
 * `Sidebar` indicator (Stage 4 Block L2) for "there's a File Manager session
 * still open in the background" — surfaces on the Connections/Transfers/
 * Settings screens (any screen other than the File Manager itself, see
 * `Sidebar`'s `hideActiveConnectionIndicator`) so the user has a one-click
 * path back to it instead of re-navigating through "Подключения" and
 * clicking "Подключиться" again (which, before this fix, also reset the
 * session's browsing state — see `App.tsx#handleConnect`).
 *
 * Uses `Plug` rather than `Cloud` (already the app logo AND the
 * "Подключения" nav icon right above this in the sidebar) to avoid visually
 * implying this is a third, unrelated "connections" affordance.
 *
 * `profileName` is required (not `string | null`) — the presence check
 * lives in `Sidebar`, which only mounts this component once there's an
 * active profile to show, keeping this component's own props simple.
 */
export function ActiveConnectionIndicator({ profileName, onClick }: ActiveConnectionIndicatorProps) {
  const { t } = useTranslation();
  return (
    <div className="px-4 py-2">
      <Tooltip content={t('sidebar.activeConnectionTooltip', { name: profileName })}>
        <button
          type="button"
          onClick={onClick}
          className="flex w-full items-center gap-2 rounded-sm bg-accent-subtle px-2.5 py-2 text-left text-xs text-accent transition-colors duration-fast hover:bg-accent-subtle/80"
        >
          <Plug className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
          <span className="truncate">{t('sidebar.activeConnection', { name: profileName })}</span>
        </button>
      </Tooltip>
    </div>
  );
}
