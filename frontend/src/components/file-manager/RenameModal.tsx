import { useState } from 'react';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { renameObject } from '../../lib/wails/fileManager';
import { getEntryDisplayName } from '../../lib/utils';
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
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Переименовать"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            Отмена
          </Button>
          <Button variant="primary" isLoading={isLoading} onClick={() => void handleRename()}>
            Переименовать
          </Button>
        </>
      }
    >
      <Input
        label="Новое имя"
        value={newName}
        onChange={(event) => setNewName(event.target.value)}
        autoFocus
      />
    </Modal>
  );
}
