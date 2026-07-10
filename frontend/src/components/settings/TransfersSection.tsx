import { Input } from '../ui/Input';
import { SegmentedControl, type SegmentedOption } from './SegmentedControl';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';
import type { AppSettings } from '../../types';

const MIN_CONCURRENCY = 1;
const MAX_CONCURRENCY = 10;

const BYTES_PER_MB = 1024 * 1024;

/** Fixed part-size choices (MB), `'0'` = adaptive (`transfer.PartSize`'s own table). */
const PART_SIZE_OPTIONS: SegmentedOption<'0' | '5' | '16' | '64' | '128'>[] = [
  { value: '0', label: 'Адаптивный' },
  { value: '5', label: '5 МБ' },
  { value: '16', label: '16 МБ' },
  { value: '64', label: '64 МБ' },
  { value: '128', label: '128 МБ' },
];

/** `0`/empty means "unlimited" — mirrors `transfer.NewBandwidthLimiter`'s own convention. */
function bytesPerSecToMbps(bytesPerSec: number): string {
  return bytesPerSec > 0 ? String(bytesPerSec / BYTES_PER_MB) : '';
}

function mbpsToBytesPerSec(input: string): number {
  const mbps = Number(input);
  return mbps > 0 ? Math.round(mbps * BYTES_PER_MB) : 0;
}

export interface TransfersSectionProps {
  value: AppSettings;
  onChange: (patch: Partial<AppSettings>) => void;
}

/**
 * "Передачи" section, per docs/03-ux-ui-spec.md section 5.7.
 *
 * Bandwidth limits are edited in MB/s here but stored (in the draft, same
 * as `domain.AppSettings`) in bytes/sec — the MB/s ⇄ bytes/sec conversion
 * happens entirely at this component's boundary, so `SettingsScreen`'s
 * draft never has to know about the display unit.
 */
export function TransfersSection({ value, onChange }: TransfersSectionProps) {
  return (
    <div className="flex flex-col">
      <SettingGroup>
        <SettingField
          label="Максимум одновременных передач"
          description={`Текущее значение: ${value.maxConcurrentTransfers}.`}
        >
          <input
            type="range"
            min={MIN_CONCURRENCY}
            max={MAX_CONCURRENCY}
            step={1}
            value={value.maxConcurrentTransfers}
            onChange={(e) => onChange({ maxConcurrentTransfers: Number(e.target.value) })}
            className="h-1 w-full max-w-xs cursor-pointer appearance-none rounded-full bg-bg-tertiary accent-accent"
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label="Размер части multipart-загрузки" description="Адаптивный подбирает размер по объёму файла.">
          <SegmentedControl
            options={PART_SIZE_OPTIONS}
            value={String(value.partSizeOverrideMB) as '0' | '5' | '16' | '64' | '128'}
            onChange={(partSize) => onChange({ partSizeOverrideMB: Number(partSize) })}
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label="Ограничение скорости, МБ/с" description="Пусто или 0 — без ограничения.">
          <div className="flex max-w-xs gap-3">
            <Input
              type="number"
              min={0}
              step="any"
              placeholder="Без ограничения"
              label="Отдача (upload)"
              value={bytesPerSecToMbps(value.bandwidthLimitUploadBytesPerSec)}
              onChange={(e) => onChange({ bandwidthLimitUploadBytesPerSec: mbpsToBytesPerSec(e.target.value) })}
            />
            <Input
              type="number"
              min={0}
              step="any"
              placeholder="Без ограничения"
              label="Приём (download)"
              value={bytesPerSecToMbps(value.bandwidthLimitDownloadBytesPerSec)}
              onChange={(e) => onChange({ bandwidthLimitDownloadBytesPerSec: mbpsToBytesPerSec(e.target.value) })}
            />
          </div>
        </SettingField>
      </SettingGroup>
    </div>
  );
}
