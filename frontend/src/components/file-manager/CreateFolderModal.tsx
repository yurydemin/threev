import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { createFolder } from '../../lib/wails/fileManager';
import { ApiError } from '../../lib/wails/errors';
import i18n from '../../i18n';

export interface CreateFolderModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
  bucket: string;
  prefix: string;
}

/** Client-side validation, mirrors the backend's own rejection (`internal/filemanager/createfolder.go`) so obviously-invalid input never round-trips. */
function validateName(name: string): string | undefined {
  if (name.trim() === '') return i18n.t('fileManager.createFolderModal.nameEmptyError');
  if (name.includes('/')) return i18n.t('fileManager.createFolderModal.nameSlashError');
  return undefined;
}

/**
 * "Создать папку" modal, opened from the Toolbar (5.4.1). Creates a
 * zero-byte "folder marker" object at `prefix + name + "/"`.
 */
export function CreateFolderModal({ isOpen, onClose, profileId, bucket, prefix }: CreateFolderModalProps) {
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
      await createFolder(profileId, bucket, prefix, name.trim());
      setName('');
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('fileManager.createFolderModal.genericError'));
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t('fileManager.createFolderModal.title')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isLoading={isLoading} onClick={() => void handleCreate()}>
            {t('fileManager.createFolderModal.create')}
          </Button>
        </>
      }
    >
      <Input
        label={t('fileManager.createFolderModal.nameLabel')}
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
