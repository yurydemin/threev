import { useTranslation } from 'react-i18next';
import { APP_VERSION } from '../../lib/appVersion';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';

/**
 * "О приложении" section, per docs/03-ux-ui-spec.md section 5.7 — trimmed
 * to just the version number (see Stage 4 Block G task notes): no GitHub
 * link (no public repo URL to honestly point at), no "check for updates"
 * (no backend mechanism for it), no license text (no LICENSE file in the
 * repository).
 */
export function AboutSection() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col">
      <SettingGroup>
        <SettingField label={t('settings.about.versionLabel')}>
          <span className="text-[13px] text-fg-primary">{APP_VERSION}</span>
        </SettingField>
      </SettingGroup>
    </div>
  );
}
