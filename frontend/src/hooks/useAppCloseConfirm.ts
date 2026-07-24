import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { EventsOn, Quit } from '../../wailsjs/runtime/runtime';
import { confirmDialog } from '../lib/confirm';
import { confirmClose } from '../lib/wails/app';
import { useSettingsStore } from '../stores/useSettingsStore';
import { useTransferStore } from '../stores/useTransferStore';
import { useFileManagerStore } from '../stores/useFileManagerStore';

const CLOSE_REQUESTED_EVENT = 'app:close-requested';
const ACTIVE_QUEUE_STATUSES = new Set(['running', 'paused', 'pending']);

/**
 * Wires up the "app:close-requested" Wails event (app.go's `beforeClose`,
 * which now always vetoes the first close attempt and lets the frontend
 * decide) into `ConfirmDialog`.
 *
 * `CloseBehavior` (`useSettingsStore`) decides WHETHER to ask at all:
 * `"confirm"` always shows the dialog, unconditionally; `"exit"` only shows
 * it when there's active work worth losing — a transfer still
 * running/paused/pending, or an open connection session
 * (`useFileManagerStore.activeProfileId`). Neither reads `useSettingsStore`
 * off a stale snapshot: `settings` is fetched once at boot by
 * `useSettingsSync` (mounted alongside this hook at the `App.tsx` root) and
 * kept current by `SettingsScreen`'s own save flow, so by the time a user
 * can actually close the window it has long since resolved.
 *
 * Mounted once at the `App.tsx` root, same rationale as `useTransferEvents`.
 */
export function useAppCloseConfirm(): void {
  const { t } = useTranslation();

  useEffect(() => {
    const off = EventsOn(CLOSE_REQUESTED_EVENT, () => {
      void (async () => {
        const behavior = useSettingsStore.getState().settings?.closeBehavior;
        const fileManagerState = useFileManagerStore.getState();
        const hasActiveWork =
          useTransferStore.getState().queue.some((task) => ACTIVE_QUEUE_STATUSES.has(task.status)) ||
          (fileManagerState.activeProfileId !== null && fileManagerState.hasConnectedOnce);
        const shouldAsk = behavior === 'confirm' || (behavior === 'exit' && hasActiveWork);

        if (shouldAsk) {
          const confirmed = await confirmDialog(t('app.closeConfirmActive'));
          if (!confirmed) return;
        }

        await confirmClose();
        Quit();
      })();
    });

    return off;
  }, [t]);
}
