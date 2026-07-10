import { clsx, type ClassValue } from 'clsx';
import { toast } from './toast';
import type { ObjectEntry } from '../types';

/**
 * Joins conditional class names. Thin wrapper around `clsx` — kept as a
 * local indirection so a `tailwind-merge` pass can be added later without
 * touching call sites.
 */
export function cn(...inputs: ClassValue[]): string {
  return clsx(inputs);
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'];

/**
 * Formats a byte count as a human-readable string (e.g. `"2.4 MB"`), per
 * docs/03-ux-ui-spec.md section 5.4.3 (Object List size column).
 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), BYTE_UNITS.length - 1);
  const value = bytes / 1024 ** exponent;
  const formatted = exponent === 0 ? String(value) : value.toFixed(value < 10 ? 2 : 1);
  return `${formatted} ${BYTE_UNITS[exponent]}`;
}

/**
 * Formats a transfer speed as a human-readable string (e.g. `"12.4 MB/s"`),
 * per docs/03-ux-ui-spec.md section 5.5 (Transfer card bottom row). Reuses
 * `formatBytes` rather than duplicating the unit-scaling logic.
 */
export function formatSpeed(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`;
}

/**
 * Formats an ETA (seconds remaining) as a compact human-readable string, per
 * docs/03-ux-ui-spec.md section 5.5. `etaSeconds < 0` means "unknown" (an
 * em dash); `0` means "already done" — callers generally don't render this
 * case (a finished transfer has no ETA row), so it resolves to an empty
 * string rather than "0с". Not ISO-8601-precise by design (e.g. `"1h 5m"`,
 * not `"1h 5m 30s"`) — this is a status-line hint, not a duration input.
 */
export function formatETA(etaSeconds: number): string {
  if (etaSeconds < 0) return '—';
  if (etaSeconds === 0) return '';
  if (etaSeconds < 60) return `${Math.round(etaSeconds)}с`;
  const totalMinutes = Math.floor(etaSeconds / 60);
  if (totalMinutes < 60) return `${totalMinutes}m`;
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return `${hours}h ${minutes}m`;
}

/**
 * Derives the display name of an `ObjectEntry` within the currently browsed
 * `currentPrefix`: strips the prefix from the front of `key`, then takes
 * the last path segment. With `ListObjects`'s delimiter-based listing,
 * `key` is always a direct child of `currentPrefix`, so the segment split
 * is a defensive no-op beyond the prefix strip — it just guards against any
 * unexpected nesting rather than assuming it away.
 *
 * Callers append the trailing `/` for folders themselves (this returns the
 * bare name) since not every caller wants it (e.g. `title=` attributes).
 */
export function getEntryDisplayName(key: string, currentPrefix: string): string {
  const relative = key.startsWith(currentPrefix) ? key.slice(currentPrefix.length) : key;
  const segments = relative.split('/').filter(Boolean);
  return segments[segments.length - 1] ?? relative;
}

/**
 * Writes `text` to the system clipboard, logging and toasting (not
 * throwing/surfacing to the caller) on failure — a denied permission or
 * unsupported environment fails gracefully rather than crashing the
 * caller's interaction. Shared by `ObjectContextMenu` ("Скопировать
 * имя"/"Скопировать путь"/"Копировать URL") and `PresignedUrlModal`
 * ("Копировать" button) — the toast is raised here, once, rather than at
 * each call site, since all of them want the same message.
 */
export async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch (err) {
    console.error('[copyToClipboard] clipboard write failed:', err);
    toast.error('Не удалось скопировать в буфер обмена');
  }
}

/**
 * Client-side search filter (FR-FM-006 / Stage 2 plan): matches `query`
 * case-insensitively against each entry's *display name* (not its full
 * key), so searching inside a nested folder doesn't match on ancestor path
 * segments. Empty/whitespace-only `query` returns `entries` unchanged.
 */
export function filterEntriesByQuery(
  entries: ObjectEntry[],
  query: string,
  currentPrefix: string,
): ObjectEntry[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return entries;
  return entries.filter((entry) =>
    getEntryDisplayName(entry.key, currentPrefix).toLowerCase().includes(normalized),
  );
}
