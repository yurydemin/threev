import { useTranslation } from 'react-i18next';
import { SegmentedControl, type SegmentedOption } from './SegmentedControl';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';
import type { ThemePreference } from '../../stores/useAppStore';
import type { AppSettings } from '../../types';

/** Fixed set of supported UI scale levels — no free-form input, per the task spec. */
const SCALE_OPTIONS: SegmentedOption<'90' | '100' | '110' | '125'>[] = [
  { value: '90', label: '90%' },
  { value: '100', label: '100%' },
  { value: '110', label: '110%' },
  { value: '125', label: '125%' },
];

export interface AppearanceSectionProps {
  value: AppSettings;
  onChange: (patch: Partial<AppSettings>) => void;
}

/**
 * "Внешний вид" section, per docs/03-ux-ui-spec.md section 5.7. Theme takes
 * effect immediately via `useTheme`/`useAppStore` reacting to the draft's
 * eventual save (see `SettingsScreen`); no font-family control here — the
 * UX spec's system/monospace toggle has no backing `AppSettings` field on
 * the backend, so it's out of scope (same "spec vs. real backend" gap
 * already hit on earlier stages).
 */
export function AppearanceSection({ value, onChange }: AppearanceSectionProps) {
  const { t } = useTranslation();
  const themeOptions: SegmentedOption<ThemePreference>[] = [
    { value: 'system', label: t('settings.appearance.themeSystem') },
    { value: 'light', label: t('settings.appearance.themeLight') },
    { value: 'dark', label: t('settings.appearance.themeDark') },
  ];

  return (
    <div className="flex flex-col">
      <SettingGroup>
        <SettingField label={t('settings.appearance.themeLabel')}>
          <SegmentedControl
            options={themeOptions}
            value={value.theme as ThemePreference}
            onChange={(theme) => onChange({ theme })}
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label={t('settings.appearance.scaleLabel')} description={t('settings.appearance.scaleDescription')}>
          <SegmentedControl
            options={SCALE_OPTIONS}
            value={String(value.uiScalePercent) as '90' | '100' | '110' | '125'}
            onChange={(scale) => onChange({ uiScalePercent: Number(scale) })}
          />
        </SettingField>
      </SettingGroup>
    </div>
  );
}
