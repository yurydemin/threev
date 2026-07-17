import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { createBucket } from '../../lib/wails/fileManager';
import { ApiError } from '../../lib/wails/errors';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import i18n from '../../i18n';

export interface CreateBucketModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
}

/**
 * Client-side validation, a soft mirror of S3's bucket-naming rules — NOT
 * the source of truth. The backend forwards straight to S3 and translates
 * `InvalidBucketName` errors itself; this only catches obviously-invalid
 * input before a round-trip (no IP-address-format check, no
 * consecutive-dots check, etc. — those edge cases are left to the backend's
 * own error message).
 */
function validateName(name: string): string | undefined {
  const trimmed = name.trim();
  if (trimmed === '') return i18n.t('fileManager.createBucketModal.nameEmptyError');
  if (trimmed.length < 3 || trimmed.length > 63) return i18n.t('fileManager.createBucketModal.nameLengthError');
  if (!/^[a-z0-9][a-z0-9.-]*[a-z0-9]$/.test(trimmed)) {
    return i18n.t('fileManager.createBucketModal.nameFormatError');
  }
  return undefined;
}

/**
 * "Создать бакет" modal (Block B), opened both from `BucketPanel`'s header
 * and `ConnectionDashboard`'s header/empty-state — each of those mounts its
 * own independent instance of this component, none of them share state.
 */
export function CreateBucketModal({ isOpen, onClose, profileId }: CreateBucketModalProps) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(false);

  async function handleCreate() {
    const validationError = validateName(name);
    if (validationError) {
      setError(validationError);
      return;
    }
    setIsLoading(true);
    setError(undefined);
    try {
      await createBucket(profileId, name.trim());
      await useFileManagerStore.getState().refreshBuckets();
      setName('');
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('fileManager.createBucketModal.genericError'));
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t('fileManager.createBucketModal.title')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isLoading={isLoading} onClick={() => void handleCreate()}>
            {t('fileManager.createBucketModal.create')}
          </Button>
        </>
      }
    >
      <Input
        label={t('fileManager.createBucketModal.nameLabel')}
        value={name}
        onChange={(event) => {
          setName(event.target.value);
          if (error) setError(undefined);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !isLoading) void handleCreate();
        }}
        error={error}
        autoFocus
      />
    </Modal>
  );
}
