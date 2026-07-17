import { useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { deleteBucket } from '../../lib/wails/fileManager';
import { ApiError } from '../../lib/wails/errors';
import { useFileManagerStore } from '../../stores/useFileManagerStore';

export interface DeleteBucketConfirmModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
  bucketName: string;
}

/**
 * Delete-bucket confirmation modal (Block B), opened from `BucketPanel`'s
 * per-row context menu — the single path for bucket deletion (see task
 * notes: `ConnectionDashboard` intentionally has no delete affordance, to
 * avoid two divergent UIs for the same destructive action).
 *
 * Unlike `DeleteConfirmModal` (which just calls a passed-in `onConfirm`
 * synchronously and closes immediately), this modal owns the actual
 * `deleteBucket()` call and its own loading/error state: `DeleteBucket`
 * requires an empty bucket, and the backend's `BucketNotEmpty` error (like
 * any other `ApiError`) needs to be shown inline rather than fire-and-forget.
 *
 * On success: if the deleted bucket is the one currently being browsed
 * (`selectedBucket`), falls back to a full `enterProfile` reset (nothing is
 * left to browse); otherwise just re-fetches the bucket list via the
 * narrower `refreshBuckets`, leaving the user's current browsing location
 * untouched.
 */
export function DeleteBucketConfirmModal({ isOpen, onClose, profileId, bucketName }: DeleteBucketConfirmModalProps) {
  const { t } = useTranslation();
  const [error, setError] = useState<string | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);

  async function handleDelete() {
    setIsLoading(true);
    setError(undefined);
    try {
      await deleteBucket(profileId, bucketName);
      const { selectedBucket, activeProfileId, activeProfileName, enterProfile, refreshBuckets } =
        useFileManagerStore.getState();
      if (selectedBucket === bucketName && activeProfileId !== null && activeProfileName !== null) {
        await enterProfile(activeProfileId, activeProfileName);
      } else {
        await refreshBuckets();
      }
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('fileManager.deleteBucketConfirmModal.genericError'));
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t('fileManager.deleteBucketConfirmModal.title')}
      footer={
        <>
          <Button variant="secondary" autoFocus onClick={onClose} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" isLoading={isLoading} onClick={() => void handleDelete()}>
            {t('common.delete')}
          </Button>
        </>
      }
    >
      <div className="flex items-start gap-3">
        <AlertTriangle className="h-8 w-8 shrink-0 text-danger" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <p className="text-[13px] text-fg-primary">
            {t('fileManager.deleteBucketConfirmModal.confirm', { name: bucketName })}
          </p>
          {error && <p className="mt-2 text-[13px] text-danger">{error}</p>}
        </div>
      </div>
    </Modal>
  );
}
