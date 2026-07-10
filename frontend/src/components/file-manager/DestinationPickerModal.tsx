import { useEffect, useState } from 'react';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Select } from '../ui/Select';
import { Input } from '../ui/Input';
import { useFileManagerStore } from '../../stores/useFileManagerStore';

export interface DestinationPickerModalProps {
  isOpen: boolean;
  onClose: () => void;
  mode: 'copy' | 'move';
  keys: string[];
  profileId: number;
  sourceBucket: string;
  onConfirm: (destBucket: string, destPrefix: string) => void;
}

/**
 * Normalizes a user-entered destination prefix into the form
 * `BulkCopyRequest`/`BulkMoveRequest.DestPrefix` expects: no leading `/`,
 * exactly one trailing `/` unless empty. Mirrors the backend's
 * `normalizeS3Prefix` (`internal/transfer/service.go`) — the backend does
 * NOT normalize `DestPrefix` itself (see `internal/filemanager/copymove.go`,
 * `destKey := params.DestPrefix + path.Base(sourceKey)`, plain
 * concatenation), so this has to happen here before the request is sent.
 */
function normalizeDestPrefix(prefix: string): string {
  const stripped = prefix.replace(/^\/+/, '');
  if (stripped === '') return '';
  return stripped.endsWith('/') ? stripped : `${stripped}/`;
}

/**
 * Destination bucket/prefix picker shared by "Копировать..." and
 * "Переместить..." (`ObjectContextMenu`'s bulk and single-file branches),
 * per docs/03-ux-ui-spec.md section 5.8. Both `BulkCopyRequest` and
 * `BulkMoveRequest` place every one of `keys` under the same flat
 * `destPrefix`, keeping each source key's own basename — this modal has no
 * per-file rename affordance (that's `RenameModal`'s job).
 */
export function DestinationPickerModal({
  isOpen,
  onClose,
  mode,
  keys,
  profileId,
  sourceBucket,
  onConfirm,
}: DestinationPickerModalProps) {
  const buckets = useFileManagerStore((state) => state.buckets);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);

  const [destBucket, setDestBucket] = useState(sourceBucket);
  const [destPrefix, setDestPrefix] = useState(currentPrefix);

  // Re-seed defaults each time the modal opens (it's mounted once and
  // toggled via `isOpen`, so stale state from a previous open would
  // otherwise leak into the next one).
  useEffect(() => {
    if (isOpen) {
      setDestBucket(sourceBucket);
      setDestPrefix(currentPrefix);
    }
  }, [isOpen, sourceBucket, currentPrefix]);

  const verb = mode === 'copy' ? 'Копировать' : 'Переместить';
  const isSingle = keys.length === 1;
  const verbGerund = isSingle
    ? mode === 'copy'
      ? 'скопирован'
      : 'перемещён'
    : mode === 'copy'
      ? 'скопированы'
      : 'перемещены';

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={mode === 'copy' ? 'Копировать объекты' : 'Переместить объекты'}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Отмена
          </Button>
          <Button
            variant="primary"
            onClick={() => {
              onConfirm(destBucket, normalizeDestPrefix(destPrefix));
              onClose();
            }}
          >
            {verb}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Select
          label="Бакет назначения"
          value={destBucket}
          onChange={setDestBucket}
          options={buckets.map((bucket) => ({ value: bucket.name, label: bucket.name }))}
        />
        <Input
          label="Папка назначения"
          value={destPrefix}
          onChange={(event) => setDestPrefix(event.target.value)}
          placeholder="например, backup/2026/"
        />
        <p className="text-2xs text-fg-muted">
          {isSingle ? 'Файл будет' : `${keys.length} файлов будут`} {verbGerund} с сохранением исходных
          имён.
        </p>
      </div>
    </Modal>
  );
}
