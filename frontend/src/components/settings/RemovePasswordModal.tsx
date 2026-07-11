import { useEffect, useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { useSecurityStore } from '../../stores/useSecurityStore';

export interface RemovePasswordModalProps {
  isOpen: boolean;
  onClose: () => void;
}

/**
 * "Удалить мастер-пароль" confirmation modal, per Stage 4 Block I.
 *
 * Same client-validate-then-call-backend, inline-error-on-failure
 * convention as `SetPasswordModal` — see its doc comment for the rationale
 * behind `useSecurityStore` skipping `toast.error` here.
 */
export function RemovePasswordModal({ isOpen, onClose }: RemovePasswordModalProps) {
  const { t } = useTranslation();
  const [currentPassword, setCurrentPassword] = useState('');
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setCurrentPassword('');
    setFieldError(null);
    setIsLoading(false);
  }, [isOpen]);

  async function handleSubmit() {
    setFieldError(null);
    if (!currentPassword) {
      setFieldError(t('settings.removePasswordModal.emptyPasswordError'));
      return;
    }

    setIsLoading(true);
    const ok = await useSecurityStore.getState().removeMasterPassword(currentPassword);
    setIsLoading(false);
    if (ok) {
      onClose();
    } else {
      setFieldError(useSecurityStore.getState().error ?? t('settings.removePasswordModal.genericError'));
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t('settings.removePasswordModal.title')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" isLoading={isLoading} onClick={() => void handleSubmit()}>
            {t('common.delete')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex items-start gap-3">
          <AlertTriangle className="h-8 w-8 shrink-0 text-danger" aria-hidden="true" />
          <p className="text-[13px] text-fg-primary">
            {t('settings.removePasswordModal.warning')}
          </p>
        </div>
        <Input
          label={t('settings.removePasswordModal.currentPasswordLabel')}
          type="password"
          value={currentPassword}
          onChange={(e) => setCurrentPassword(e.target.value)}
          error={fieldError ?? undefined}
          autoFocus
        />
      </div>
    </Modal>
  );
}
