import { useState } from 'react';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { createFolder } from '../../lib/wails/fileManager';
import { ApiError } from '../../lib/wails/errors';

export interface CreateFolderModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
  bucket: string;
  prefix: string;
}

/** Client-side validation, mirrors the backend's own rejection (`internal/filemanager/createfolder.go`) so obviously-invalid input never round-trips. */
function validateName(name: string): string | undefined {
  if (name.trim() === '') return 'Введите имя папки';
  if (name.includes('/')) return 'Имя не может содержать «/»';
  return undefined;
}

/**
 * "Создать папку" modal, opened from the Toolbar (5.4.1). Creates a
 * zero-byte "folder marker" object at `prefix + name + "/"`.
 */
export function CreateFolderModal({ isOpen, onClose, profileId, bucket, prefix }: CreateFolderModalProps) {
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
      setError(err instanceof ApiError ? err.message : 'Не удалось создать папку');
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Создать папку"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            Отмена
          </Button>
          <Button variant="primary" isLoading={isLoading} onClick={() => void handleCreate()}>
            Создать
          </Button>
        </>
      }
    >
      <Input
        label="Имя папки"
        value={name}
        onChange={(event) => {
          setName(event.target.value);
          if (error) setError(undefined);
        }}
        error={error}
        autoFocus
      />
    </Modal>
  );
}
