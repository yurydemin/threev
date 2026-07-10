import { pickUploadFiles } from './wails/transfer';
import { useTransferStore } from '../stores/useTransferStore';
import { toast } from './toast';

/**
 * Opens the native "pick files" dialog and, if the user selected at least
 * one file, queues them for upload to `bucket`/`prefix`.
 *
 * Shared by the three places that offer the same "Выбрать файлы…" action
 * per docs/03-ux-ui-spec.md sections 5.4.1 (Toolbar "Загрузить" dropdown)
 * and 5.4.3 (Object List empty state "Загрузить файлы" button, both list
 * and grid views) — pulled out here once a third call site appeared, rather
 * than duplicating the pick → check-cancelled → queue → catch sequence a
 * third time.
 *
 * No-op if the user cancels the dialog (`pickUploadFiles()` resolves to an
 * empty array) or if `profileId`/`bucket` are unset (nothing to upload to).
 */
export async function pickAndQueueUploadFiles(
  profileId: number | null,
  bucket: string | null,
  prefix: string,
): Promise<void> {
  if (!profileId || !bucket) return;
  try {
    const paths = await pickUploadFiles();
    if (paths.length === 0) return;
    await useTransferStore.getState().queueUploadPaths(profileId, bucket, prefix, paths);
  } catch (err) {
    console.error('[uploadFiles] pickAndQueueUploadFiles failed:', err);
    toast.error('Не удалось начать загрузку файлов');
  }
}
