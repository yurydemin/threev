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
import {
  GetSettings,
  SaveSettings,
  HasMasterPassword,
  IsLocked,
  Unlock,
  SetMasterPassword,
  RemoveMasterPassword,
} from '../../../wailsjs/go/appsettings/SettingsService';
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

/** Whether a master password has been set up (independent of lock state). */
export async function hasMasterPassword(): Promise<boolean> {
  return call(() => HasMasterPassword());
}

/** Whether the app is currently locked (a master password is set and hasn't been unlocked this session). */
export async function isLocked(): Promise<boolean> {
  return call(() => IsLocked());
}

/** Attempts to unlock with `password`. Resolves `false` (not a rejection) on a wrong password. */
export async function unlock(password: string): Promise<boolean> {
  return call(() => Unlock(password));
}

/** Sets (or changes) the master password. No current-password check when already unlocked — see `SecuritySection`. */
export async function setMasterPassword(password: string): Promise<void> {
  return call(() => SetMasterPassword(password));
}

/** Removes the master password, requiring `currentPassword` for confirmation. */
export async function removeMasterPassword(currentPassword: string): Promise<void> {
  return call(() => RemoveMasterPassword(currentPassword));
}
