import i18n from '../i18n';
import { toast } from './toast';
import { ApiError } from './wails/errors';
import { pickDownloadDirectory } from './wails/transfer';
import { useTransferStore } from '../stores/useTransferStore';

/**
 * Shared "download N selected objects" flow — prompts for a destination
 * directory once, then queues one download per key into it. Extracted so
 * `ObjectContextMenu`'s bulk branch and `Toolbar`'s selection actions (Stage
 * 4, Block L3) share a single implementation instead of two copies of the
 * same `pickDownloadDirectory().then(...)` chain.
 *
 * Not a React hook/component, so it reaches for the `toast`/`i18n` singletons
 * directly rather than accepting them as props — same convention already
 * used by `lib/utils.ts` (`copyToClipboard`) and `lib/wails/errors.ts`.
 */
export async function downloadSelectedObjects(
  profileId: number,
  bucket: string,
  keys: string[],
): Promise<void> {
  try {
    const dir = await pickDownloadDirectory();
    if (!dir) return;
    // Each `queueDownload` swallows its own errors internally
    // (`useTransferStore.queueDownload` catches and returns `null` rather
    // than rejecting), so one failing key never stops the rest of the loop
    // from being queued.
    for (const key of keys) {
      void useTransferStore.getState().queueDownload({
        profileId,
        bucket,
        key,
        localPath: `${dir}/${key.split('/').pop()}`,
        priority: 0,
      });
    }
  } catch (err) {
    console.error('[downloadSelectedObjects] pickDownloadDirectory failed:', err);
    toast.error(
      err instanceof ApiError ? err.message : i18n.t('fileManager.objectContextMenu.pickDownloadDirError'),
      err instanceof ApiError ? err.raw : undefined,
    );
  }
}
