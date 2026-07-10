import { Checkbox } from '../ui/Checkbox';
import { Select, type SelectOption } from '../ui/Select';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';
import type { AppSettings } from '../../types';

const CLOSE_BEHAVIOR_OPTIONS: SelectOption[] = [
  { value: 'exit', label: 'Выйти' },
  { value: 'confirm', label: 'Спросить подтверждение' },
];

export interface GeneralSectionProps {
  value: AppSettings;
  onChange: (patch: Partial<AppSettings>) => void;
}

/**
 * "Общие" section, per docs/03-ux-ui-spec.md section 5.7. Holds no state of
 * its own — reads/writes the screen-level draft via `value`/`onChange`, the
 * same convention as every other section here.
 */
export function GeneralSection({ value, onChange }: GeneralSectionProps) {
  return (
    <div className="flex flex-col">
      <SettingGroup>
        <SettingField
          label="При закрытии окна"
          description="Действие при нажатии на кнопку закрытия окна приложения."
        >
          <Select
            options={CLOSE_BEHAVIOR_OPTIONS}
            value={value.closeBehavior}
            onChange={(closeBehavior) => onChange({ closeBehavior })}
            className="max-w-xs"
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label="Очередь передач">
          <Checkbox
            label="Автоматически возобновлять передачи при запуске"
            checked={value.autoResumeQueue}
            onChange={(e) => onChange({ autoResumeQueue: e.target.checked })}
          />
        </SettingField>
      </SettingGroup>
    </div>
  );
}
