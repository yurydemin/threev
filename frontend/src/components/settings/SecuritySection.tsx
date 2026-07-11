import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '../ui/Button';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';
import { SetPasswordModal } from './SetPasswordModal';
import { RemovePasswordModal } from './RemovePasswordModal';
import { useSecurityStore } from '../../stores/useSecurityStore';

/**
 * "Безопасность" section, per Stage 4 Block I.
 *
 * Unlike `GeneralSection`/`AppearanceSection`/`TransfersSection`, the
 * master password is NOT a field of `AppSettings` — it's an immediately
 * applied backend operation, not a draft edited by `SettingsScreen`'s
 * screen-wide "Сохранить изменения" button. This component is therefore
 * fully self-contained: no `value`/`onChange` props, own data fetch (via
 * `useSecurityStore`, lazily on mount — see that store's doc comment for
 * why this differs from `useSettingsSync`'s eager root-level fetch).
 *
 * No auto-lock/timeout toggle here by design — the backend has no such
 * mechanism yet.
 */
export function SecuritySection() {
  const { t } = useTranslation();
  const hasMasterPassword = useSecurityStore((state) => state.hasMasterPassword);
  const [setModalMode, setSetModalMode] = useState<'set' | 'change' | null>(null);
  const [isRemoveModalOpen, setIsRemoveModalOpen] = useState(false);

  useEffect(() => {
    void useSecurityStore.getState().fetchHasMasterPassword();
  }, []);

  if (hasMasterPassword === null) {
    return <p className="text-sm text-fg-muted">{t('common.loading')}</p>;
  }

  return (
    <div className="flex flex-col">
      <SettingGroup>
        {hasMasterPassword ? (
          <SettingField
            label={t('settings.security.masterPasswordLabel')}
            description={t('settings.security.setDescription')}
          >
            <div className="flex items-center gap-2">
              <Button variant="secondary" onClick={() => setSetModalMode('change')}>
                {t('settings.security.changePassword')}
              </Button>
              <Button variant="danger" onClick={() => setIsRemoveModalOpen(true)}>
                {t('settings.security.removePassword')}
              </Button>
            </div>
          </SettingField>
        ) : (
          <SettingField
            label={t('settings.security.masterPasswordLabel')}
            description={t('settings.security.unsetDescription')}
          >
            <Button variant="primary" onClick={() => setSetModalMode('set')}>
              {t('settings.security.setPassword')}
            </Button>
          </SettingField>
        )}
      </SettingGroup>

      <SetPasswordModal
        isOpen={setModalMode !== null}
        onClose={() => setSetModalMode(null)}
        mode={setModalMode ?? 'set'}
      />
      <RemovePasswordModal isOpen={isRemoveModalOpen} onClose={() => setIsRemoveModalOpen(false)} />
    </div>
  );
}
