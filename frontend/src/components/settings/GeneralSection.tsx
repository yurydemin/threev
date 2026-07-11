import { useTranslation } from 'react-i18next';
import { Checkbox } from '../ui/Checkbox';
import { Select, type SelectOption } from '../ui/Select';
import { SegmentedControl, type SegmentedOption } from './SegmentedControl';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';
import { useAppStore, type Language } from '../../stores/useAppStore';
import type { AppSettings } from '../../types';

export interface GeneralSectionProps {
  value: AppSettings;
  onChange: (patch: Partial<AppSettings>) => void;
}

/** Language names are shown in their own language, not translated per the active UI language. */
const LANGUAGE_OPTIONS: SegmentedOption<Language>[] = [
  { value: 'ru', label: 'Русский' },
  { value: 'en', label: 'English' },
];

/**
 * "Общие" section, per docs/03-ux-ui-spec.md section 5.7. Holds no state of
 * its own for the `AppSettings` fields — reads/writes the screen-level
 * draft via `value`/`onChange`, the same convention as every other section
 * here.
 *
 * The language switcher below is the one exception: language (Stage 4 Block
 * K) is NOT part of `AppSettings` (it's a pure frontend preference, see
 * `useAppStore`'s `language`), so it reads/writes `useAppStore` directly —
 * same self-contained-island pattern `SecuritySection` already established
 * for the master password (see that component's doc comment).
 */
export function GeneralSection({ value, onChange }: GeneralSectionProps) {
  const { t } = useTranslation();
  const language = useAppStore((state) => state.language);
  const setLanguage = useAppStore((state) => state.setLanguage);

  const closeBehaviorOptions: SelectOption[] = [
    { value: 'exit', label: t('settings.general.closeBehaviorExit') },
    { value: 'confirm', label: t('settings.general.closeBehaviorConfirm') },
  ];

  return (
    <div className="flex flex-col">
      <SettingGroup>
        <SettingField
          label={t('settings.general.closeBehaviorLabel')}
          description={t('settings.general.closeBehaviorDescription')}
        >
          <Select
            options={closeBehaviorOptions}
            value={value.closeBehavior}
            onChange={(closeBehavior) => onChange({ closeBehavior })}
            className="max-w-xs"
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label={t('settings.general.transferQueueLabel')}>
          <Checkbox
            label={t('settings.general.autoResumeLabel')}
            checked={value.autoResumeQueue}
            onChange={(e) => onChange({ autoResumeQueue: e.target.checked })}
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label={t('settings.general.languageLabel')}>
          <SegmentedControl options={LANGUAGE_OPTIONS} value={language} onChange={setLanguage} />
        </SettingField>
      </SettingGroup>
    </div>
  );
}
