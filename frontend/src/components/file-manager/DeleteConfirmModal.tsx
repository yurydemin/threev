import { AlertTriangle } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { getEntryDisplayName } from '../../lib/utils';

export interface DeleteConfirmModalProps {
  isOpen: boolean;
  onClose: () => void;
  keys: string[];
  /** Starts the bulk delete and closes the modal immediately — progress is shown by `BulkProgressOverlay`, this modal never waits for completion. */
  onConfirm: () => void;
}

/**
 * Delete confirmation modal, per docs/03-ux-ui-spec.md section 5.8.1.
 *
 * Reads `currentPrefix` from `useFileManagerStore` (not a prop) purely to
 * derive display names via `getEntryDisplayName`, the same convention
 * `ObjectContextMenu` already uses.
 */
export function DeleteConfirmModal({ isOpen, onClose, keys, onConfirm }: DeleteConfirmModalProps) {
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);

  const isSingle = keys.length === 1;
  const singleName = isSingle ? getEntryDisplayName(keys[0], currentPrefix) : '';

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Удалить объекты"
      footer={
        <>
          <Button variant="secondary" autoFocus onClick={onClose}>
            Отмена
          </Button>
          <Button
            variant="danger"
            onClick={() => {
              onConfirm();
              onClose();
            }}
          >
            Удалить
          </Button>
        </>
      }
    >
      <div className="flex items-start gap-3">
        <AlertTriangle className="h-8 w-8 shrink-0 text-danger" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <p className="text-[13px] text-fg-primary">
            {isSingle
              ? `Вы уверены, что хотите удалить объект «${singleName}»?`
              : `Вы уверены, что хотите удалить ${keys.length} объектов?`}
          </p>
          {!isSingle && (
            <ul className="mt-2 max-h-[200px] overflow-y-auto rounded border border-border bg-bg-secondary p-2">
              {keys.map((key) => (
                <li key={key} className="truncate py-0.5 text-[13px] text-fg-secondary">
                  {getEntryDisplayName(key, currentPrefix)}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Modal>
  );
}
