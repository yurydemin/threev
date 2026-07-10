/**
 * Typed wrapper around the generated
 * `wailsjs/go/appsettings/SettingsService` bindings.
 *
 * Same conventions as `lib/wails/fileManager.ts`: converts between the
 * frontend-domain `AppSettings` (`types/index.ts`, camelCase) and the
 * wailsjs-generated `domain.AppSettings` DTO class (PascalCase), and
 * normalizes rejected promises into `ApiError` via `call`.
 *
 * `ApplySettings` is deliberately NOT imported/re-exported here: it's bound
 * by Wails because `internal/app.go` calls it internally (to live-apply a
 * just-saved setting, e.g. bandwidth limiter reconfiguration) without a
 * restart, but the frontend has no legitimate reason to invoke it directly
 * — `SaveSettings` is the only write path a screen should ever call.
 */
import { GetSettings, SaveSettings } from '../../../wailsjs/go/appsettings/SettingsService';
import { domain } from '../../../wailsjs/go/models';
import type { AppSettings } from '../../types';
import { call } from './errors';

function fromAppSettings(s: domain.AppSettings): AppSettings {
  return {
    theme: s.Theme,
    uiScalePercent: s.UIScalePercent,
    closeBehavior: s.CloseBehavior,
    autoResumeQueue: s.AutoResumeQueue,
    maxConcurrentTransfers: s.MaxConcurrentTransfers,
    partSizeOverrideMB: s.PartSizeOverrideMB,
    bandwidthLimitUploadBytesPerSec: s.BandwidthLimitUploadBytesPerSec,
    bandwidthLimitDownloadBytesPerSec: s.BandwidthLimitDownloadBytesPerSec,
  };
}

function toAppSettings(s: AppSettings): domain.AppSettings {
  return domain.AppSettings.createFrom({
    Theme: s.theme,
    UIScalePercent: s.uiScalePercent,
    CloseBehavior: s.closeBehavior,
    AutoResumeQueue: s.autoResumeQueue,
    MaxConcurrentTransfers: s.maxConcurrentTransfers,
    PartSizeOverrideMB: s.partSizeOverrideMB,
    BandwidthLimitUploadBytesPerSec: s.bandwidthLimitUploadBytesPerSec,
    BandwidthLimitDownloadBytesPerSec: s.bandwidthLimitDownloadBytesPerSec,
  });
}

export async function getSettings(): Promise<AppSettings> {
  return call(async () => fromAppSettings(await GetSettings()));
}

export async function saveSettings(settings: AppSettings): Promise<void> {
  return call(() => SaveSettings(toAppSettings(settings)));
}
