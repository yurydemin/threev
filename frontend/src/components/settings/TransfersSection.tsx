import { useTranslation } from 'react-i18next';
import { Input } from '../ui/Input';
import { SegmentedControl, type SegmentedOption } from './SegmentedControl';
import { SettingField } from './SettingField';
import { SettingGroup } from './SettingGroup';
import type { AppSettings } from '../../types';

const MIN_CONCURRENCY = 1;
const MAX_CONCURRENCY = 10;

const MIN_RETRY_ATTEMPTS = 1;
const MAX_RETRY_ATTEMPTS = 10;

const BYTES_PER_MB = 1024 * 1024;

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
  const { t } = useTranslation();

  /** Fixed part-size choices (MB), `'0'` = adaptive (`transfer.PartSize`'s own table). */
  const partSizeOptions: SegmentedOption<'0' | '5' | '16' | '64' | '128'>[] = [
    { value: '0', label: t('settings.transfers.partSizeAdaptive') },
    { value: '5', label: t('settings.transfers.mb', { size: 5 }) },
    { value: '16', label: t('settings.transfers.mb', { size: 16 }) },
    { value: '64', label: t('settings.transfers.mb', { size: 64 }) },
    { value: '128', label: t('settings.transfers.mb', { size: 128 }) },
  ];

  return (
    <div className="flex flex-col">
      <SettingGroup>
        <SettingField
          label={t('settings.transfers.maxConcurrentLabel')}
          description={t('settings.transfers.maxConcurrentDescription', { count: value.maxConcurrentTransfers })}
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
        <SettingField label={t('settings.transfers.partSizeLabel')} description={t('settings.transfers.partSizeDescription')}>
          <SegmentedControl
            options={partSizeOptions}
            value={String(value.partSizeOverrideMB) as '0' | '5' | '16' | '64' | '128'}
            onChange={(partSize) => onChange({ partSizeOverrideMB: Number(partSize) })}
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField label={t('settings.transfers.bandwidthLabel')} description={t('settings.transfers.bandwidthDescription')}>
          <div className="flex max-w-xs gap-3">
            <Input
              type="number"
              min={0}
              step="any"
              placeholder={t('settings.transfers.unlimitedPlaceholder')}
              label={t('settings.transfers.uploadLabel')}
              value={bytesPerSecToMbps(value.bandwidthLimitUploadBytesPerSec)}
              onChange={(e) => onChange({ bandwidthLimitUploadBytesPerSec: mbpsToBytesPerSec(e.target.value) })}
            />
            <Input
              type="number"
              min={0}
              step="any"
              placeholder={t('settings.transfers.unlimitedPlaceholder')}
              label={t('settings.transfers.downloadLabel')}
              value={bytesPerSecToMbps(value.bandwidthLimitDownloadBytesPerSec)}
              onChange={(e) => onChange({ bandwidthLimitDownloadBytesPerSec: mbpsToBytesPerSec(e.target.value) })}
            />
          </div>
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField
          label={t('settings.transfers.retryAttemptsLabel')}
          description={t('settings.transfers.retryAttemptsDescription', { count: value.retryMaxAttempts })}
        >
          <input
            type="range"
            min={MIN_RETRY_ATTEMPTS}
            max={MAX_RETRY_ATTEMPTS}
            step={1}
            value={value.retryMaxAttempts}
            onChange={(e) => onChange({ retryMaxAttempts: Number(e.target.value) })}
            className="h-1 w-full max-w-xs cursor-pointer appearance-none rounded-full bg-bg-tertiary accent-accent"
          />
        </SettingField>
      </SettingGroup>

      <SettingGroup>
        <SettingField
          label={t('settings.transfers.connectionTimeoutLabel')}
          description={t('settings.transfers.connectionTimeoutDescription')}
        >
          <Input
            type="number"
            min={10}
            max={120}
            step={1}
            value={value.connectionTimeoutSeconds}
            onChange={(e) => onChange({ connectionTimeoutSeconds: Number(e.target.value) })}
          />
        </SettingField>
      </SettingGroup>
    </div>
  );
}
