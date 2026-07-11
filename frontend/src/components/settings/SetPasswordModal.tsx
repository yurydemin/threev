import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { useSecurityStore } from '../../stores/useSecurityStore';

const MIN_PASSWORD_LENGTH = 8;

export interface SetPasswordModalProps {
  isOpen: boolean;
  onClose: () => void;
  /**
   * `'set'` (no master password configured yet) and `'change'` (replacing
   * an existing one) share this component: the backend call is the same
   * `SetMasterPassword(password)` in both cases (it doesn't require the
   * current password when the app is already unlocked), only the copy
   * differs.
   */
  mode: 'set' | 'change';
}

/**
 * "Установить"/"Сменить мастер-пароль" modal, per Stage 4 Block I.
 *
 * Validates client-side (length + confirmation match) before ever calling
 * the backend — same "click-to-validate, error shown on the field, button
 * never fully disabled" convention as `RenameModal`/`DeleteConfirmModal`.
 * On a backend error, the message is kept in local `error` state and shown
 * inline (the modal stays open so the user can correct their input without
 * retyping) — `useSecurityStore` deliberately skips `toast.error` for this
 * reason, see its own doc comment.
 */
export function SetPasswordModal({ isOpen, onClose, mode }: SetPasswordModalProps) {
  const { t } = useTranslation();
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [confirmFieldError, setConfirmFieldError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setPassword('');
    setConfirmPassword('');
    setFieldError(null);
    setConfirmFieldError(null);
    setIsLoading(false);
  }, [isOpen]);

  async function handleSubmit() {
    setFieldError(null);
    setConfirmFieldError(null);

    if (password.length < MIN_PASSWORD_LENGTH) {
      setFieldError(t('settings.setPasswordModal.tooShortError', { min: MIN_PASSWORD_LENGTH }));
      return;
    }
    if (password !== confirmPassword) {
      setConfirmFieldError(t('settings.setPasswordModal.mismatchError'));
      return;
    }

    setIsLoading(true);
    const ok = await useSecurityStore.getState().setMasterPassword(password);
    setIsLoading(false);
    if (ok) {
      onClose();
    } else {
      setFieldError(useSecurityStore.getState().error ?? t('settings.setPasswordModal.genericError'));
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={mode === 'set' ? t('settings.setPasswordModal.titleSet') : t('settings.setPasswordModal.titleChange')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isLoading={isLoading} onClick={() => void handleSubmit()}>
            {mode === 'set' ? t('settings.setPasswordModal.submitSet') : t('settings.setPasswordModal.submitChange')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Input
          label={t('settings.setPasswordModal.newPasswordLabel')}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={fieldError ?? undefined}
          autoFocus
        />
        <Input
          label={t('settings.setPasswordModal.confirmPasswordLabel')}
          type="password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          error={confirmFieldError ?? undefined}
        />
      </div>
    </Modal>
  );
}
