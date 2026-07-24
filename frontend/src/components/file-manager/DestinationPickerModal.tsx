import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Select } from '../ui/Select';
import { Input } from '../ui/Input';
import { Checkbox } from '../ui/Checkbox';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { useConnectionStore } from '../../stores/useConnectionStore';
import { listBuckets } from '../../lib/wails/fileManager';
import { ApiError } from '../../lib/wails/errors';
import { confirmDialog } from '../../lib/confirm';
import { formatBytes } from '../../lib/utils';
import type { Bucket } from '../../types';
import { FolderTree } from './FolderTree';

export interface DestinationPickerModalProps {
  isOpen: boolean;
  onClose: () => void;
  mode: 'copy' | 'move';
  keys: string[];
  profileId: number;
  sourceBucket: string;
  /**
   * `destProfileId` is only passed when the user opted into "другое
   * подключение" (cross-connection) mode — `undefined` (the default,
   * toggle-off case) means the caller should keep routing through the
   * existing same-profile `copyObjects`/`moveObjects` fast path unchanged.
   */
  onConfirm: (destBucket: string, destPrefix: string, destProfileId?: number) => void;
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

/** Sums `entry.size` for every key in `keys`, looked up from the currently loaded source listing — used only for the cross-connection disk-space warning (bulk selection already excludes folders, so every key here is a real object with a known size). */
function sumEntrySizes(keys: string[]): number {
  const entries = useFileManagerStore.getState().entries;
  const sizeByKey = new Map(entries.map((entry) => [entry.key, entry.size]));
  return keys.reduce((total, key) => total + (sizeByKey.get(key) ?? 0), 0);
}

/**
 * Destination bucket/prefix picker shared by "Копировать..." and
 * "Переместить..." (`ObjectContextMenu`'s bulk and single-file branches),
 * per docs/03-ux-ui-spec.md section 5.8. Both `BulkCopyRequest` and
 * `BulkMoveRequest` place every one of `keys` under the same flat
 * `destPrefix`, keeping each source key's own basename — this modal has no
 * per-file rename affordance (that's `RenameModal`'s job).
 *
 * Cross-connection mode (Этап 8 Block B): an opt-in toggle, default OFF,
 * that reveals a destination-connection `Select` (every saved profile
 * except `profileId` itself — picking the source profile again would
 * defeat the point, and should just use the same-profile fast path
 * instead). While it's on, the destination-bucket `Select`/`FolderTree`
 * re-target the chosen profile via a LOCAL `crossBuckets` fetch
 * (`ListBuckets(destProfileId)`, mirroring `FolderTree`'s own local-state
 * pattern) rather than `useFileManagerStore`'s `buckets`, which stays
 * scoped to the currently active profile and must not be mutated by
 * browsing a destination picker.
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
  const { t } = useTranslation();
  const buckets = useFileManagerStore((state) => state.buckets);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);
  const connections = useConnectionStore((state) => state.connections);

  const [destBucket, setDestBucket] = useState(sourceBucket);
  const [destPrefix, setDestPrefix] = useState(currentPrefix);

  const [isCrossConnection, setIsCrossConnection] = useState(false);
  const [crossDestProfileId, setCrossDestProfileId] = useState<number | null>(null);
  const [crossBuckets, setCrossBuckets] = useState<Bucket[]>([]);
  const [isLoadingCrossBuckets, setIsLoadingCrossBuckets] = useState(false);
  const [crossBucketsError, setCrossBucketsError] = useState<string | null>(null);

  // Re-seed defaults each time the modal opens (it's mounted once and
  // toggled via `isOpen`, so stale state from a previous open — including a
  // previous cross-connection selection — would otherwise leak into the
  // next one).
  useEffect(() => {
    if (isOpen) {
      setDestBucket(sourceBucket);
      setDestPrefix(currentPrefix);
      setIsCrossConnection(false);
      setCrossDestProfileId(null);
      setCrossBuckets([]);
      setCrossBucketsError(null);
    }
  }, [isOpen, sourceBucket, currentPrefix]);

  // `destBucket`/`destPrefix` belong to whichever profile's bucket
  // namespace was last browsed — stale the moment the destination profile
  // itself changes (toggled on, toggled back off, or switched to a
  // different profile), so both are explicitly cleared by these two
  // handlers rather than carrying over a bucket name that may not even
  // exist on the new target. Deliberately handlers, not a reactive `useEffect`
  // keyed off `[isCrossConnection, crossDestProfileId]`: that would also
  // fire on this component's very first mount (alongside the `isOpen`
  // reset effect above, in the same commit), clobbering the freshly-seeded
  // `sourceBucket`/`currentPrefix` defaults back to empty strings.
  function handleToggleCrossConnection(next: boolean) {
    setIsCrossConnection(next);
    setCrossDestProfileId(null);
    setCrossBuckets([]);
    setCrossBucketsError(null);
    setDestBucket(next ? '' : sourceBucket);
    setDestPrefix(next ? '' : currentPrefix);
  }

  function handleSelectCrossDestProfile(id: number) {
    setCrossDestProfileId(id);
    setDestBucket('');
    setDestPrefix('');
  }

  useEffect(() => {
    if (!isCrossConnection || crossDestProfileId === null) return;
    let cancelled = false;
    setIsLoadingCrossBuckets(true);
    setCrossBucketsError(null);
    listBuckets(crossDestProfileId)
      .then((result) => {
        if (!cancelled) setCrossBuckets(result);
      })
      .catch((err) => {
        if (cancelled) return;
        setCrossBuckets([]);
        setCrossBucketsError(err instanceof ApiError ? err.message : t('fileManager.destinationPickerModal.crossBucketsLoadError'));
      })
      .finally(() => {
        if (!cancelled) setIsLoadingCrossBuckets(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isCrossConnection, crossDestProfileId, t]);

  const verb = mode === 'copy' ? t('fileManager.destinationPickerModal.verbCopy') : t('fileManager.destinationPickerModal.verbMove');
  const isSingle = keys.length === 1;
  const verbGerund = isSingle
    ? mode === 'copy'
      ? t('fileManager.destinationPickerModal.gerundCopySingle')
      : t('fileManager.destinationPickerModal.gerundMoveSingle')
    : mode === 'copy'
      ? t('fileManager.destinationPickerModal.gerundCopyMultiple')
      : t('fileManager.destinationPickerModal.gerundMoveMultiple');

  const destProfileOptions = connections
    .filter((connection) => connection.id !== profileId)
    .map((connection) => ({ value: String(connection.id), label: connection.name }));

  const effectiveProfileId = isCrossConnection && crossDestProfileId !== null ? crossDestProfileId : profileId;
  const bucketOptions = (isCrossConnection ? crossBuckets : buckets).map((bucket) => ({ value: bucket.name, label: bucket.name }));
  const canShowBucketPicker = !isCrossConnection || crossDestProfileId !== null;
  const canConfirm = !isCrossConnection || (crossDestProfileId !== null && destBucket !== '');

  /**
   * Same-profile branch (`!isCrossConnection`, the default) is byte-for-byte
   * what this handler used to do inline: `onConfirm(destBucket,
   * normalizeDestPrefix(destPrefix))` then `onClose()`, no disk-space check
   * — that check only exists in the cross-connection branch below, per the
   * "Файлы будут временно скопированы на локальный диск" warning (Block B),
   * since only a cross-connection copy/move stages through the local disk
   * at all (same-profile is a pure server-side CopyObject). Cancelling the
   * warning leaves the modal open exactly as-is (no `onClose()` call).
   */
  async function handleConfirm() {
    if (isCrossConnection && crossDestProfileId !== null) {
      const totalBytes = sumEntrySizes(keys);
      const confirmed = await confirmDialog(
        t('fileManager.destinationPickerModal.crossConnectionDiskWarning', { size: formatBytes(totalBytes) }),
      );
      if (!confirmed) return;
      onConfirm(destBucket, normalizeDestPrefix(destPrefix), crossDestProfileId);
      onClose();
      return;
    }
    onConfirm(destBucket, normalizeDestPrefix(destPrefix));
    onClose();
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      size="large"
      title={mode === 'copy' ? t('fileManager.destinationPickerModal.titleCopy') : t('fileManager.destinationPickerModal.titleMove')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" disabled={!canConfirm} onClick={() => void handleConfirm()}>
            {verb}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Checkbox
          label={t('fileManager.destinationPickerModal.crossConnectionToggleLabel')}
          checked={isCrossConnection}
          onChange={(event) => handleToggleCrossConnection(event.target.checked)}
        />

        {isCrossConnection && (
          <Select
            label={t('fileManager.destinationPickerModal.destinationProfileLabel')}
            placeholder={t('fileManager.destinationPickerModal.destinationProfilePlaceholder')}
            value={crossDestProfileId !== null ? String(crossDestProfileId) : ''}
            onChange={(value) => handleSelectCrossDestProfile(Number(value))}
            options={destProfileOptions}
          />
        )}

        {canShowBucketPicker && (
          <>
            <Select
              label={t('fileManager.destinationPickerModal.bucketLabel')}
              value={destBucket}
              onChange={setDestBucket}
              options={bucketOptions}
              disabled={isCrossConnection && isLoadingCrossBuckets}
            />
            {isCrossConnection && crossBucketsError && <p className="text-2xs text-danger">{crossBucketsError}</p>}
            {destBucket && (
              <div className="flex flex-col gap-1">
                <span className="text-xs font-medium text-fg-secondary">{t('fileManager.destinationPickerModal.treeLabel')}</span>
                <FolderTree profileId={effectiveProfileId} bucket={destBucket} selectedPrefix={destPrefix} onSelect={setDestPrefix} />
              </div>
            )}
            <Input
              label={t('fileManager.destinationPickerModal.prefixLabel')}
              value={destPrefix}
              onChange={(event) => setDestPrefix(event.target.value)}
              placeholder={t('fileManager.destinationPickerModal.prefixPlaceholder')}
            />
          </>
        )}
        <p className="text-2xs text-fg-muted">
          {isSingle
            ? t('fileManager.destinationPickerModal.resultSingle')
            : t('fileManager.destinationPickerModal.resultMultiple', { count: keys.length })}{' '}
          {verbGerund} {t('fileManager.destinationPickerModal.resultSuffix')}
        </p>
      </div>
    </Modal>
  );
}
