import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { renameObject } from '../../lib/wails/fileManager';
import { getEntryDisplayName } from '../../lib/utils';
import { toast } from '../../lib/toast';
import { ApiError } from '../../lib/wails/errors';
import type { ObjectEntry } from '../../types';

export interface RenameModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
  bucket: string;
  entry: ObjectEntry;
  currentPrefix: string;
  /** Called after a successful rename, in addition to `onClose` (e.g. to clear a stale selection). */
  onRenamed?: () => void;
}

/**
 * Single-object rename modal, per docs/03-ux-ui-spec.md section 5.8.
 *
 * Only changes the object's basename, never its folder: `newKey` is built
 * as `currentPrefix + newName`, the same prefix `entry` already lives under.
 * Moving an object to a different folder is `DestinationPickerModal`'s job
 * (via `moveObjects`) — `moveObjects`/`copyObjects` can't rename (see
 * `lib/wails/fileManager.ts#renameObject`'s doc comment), and this modal
 * can't relocate, by design: the two operations use different backend RPCs
 * with genuinely different semantics.
 *
 * Synchronous, single-object call — no operation id / progress event, so
 * this just tracks its own `isLoading` state on the confirm button rather
 * than going through `useBulkOperationStore`.
 */
export function RenameModal({ isOpen, onClose, profileId, bucket, entry, currentPrefix, onRenamed }: RenameModalProps) {
  const { t } = useTranslation();
  const [newName, setNewName] = useState(() => getEntryDisplayName(entry.key, currentPrefix));
  const [isLoading, setIsLoading] = useState(false);

  async function handleRename() {
    const trimmed = newName.trim();
    if (!trimmed) return;
    setIsLoading(true);
    try {
      await renameObject(profileId, bucket, entry.key, `${currentPrefix}${trimmed}`);
      onRenamed?.();
      onClose();
    } catch (err) {
      console.error('[RenameModal] renameObject failed:', err);
      toast.error(
        err instanceof ApiError ? err.message : t('fileManager.renameModal.genericError'),
        err instanceof ApiError ? err.raw : undefined,
      );
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t('fileManager.renameModal.title')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isLoading={isLoading} onClick={() => void handleRename()}>
            {t('fileManager.renameModal.title')}
          </Button>
        </>
      }
    >
      <Input
        label={t('fileManager.renameModal.nameLabel')}
        value={newName}
        onChange={(event) => setNewName(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !isLoading) void handleRename();
        }}
        autoFocus
      />
    </Modal>
  );
}
