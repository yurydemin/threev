import { clsx, type ClassValue } from 'clsx';
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
