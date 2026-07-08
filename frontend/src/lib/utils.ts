import { clsx, type ClassValue } from 'clsx';

/**
 * Joins conditional class names. Thin wrapper around `clsx` — kept as a
 * local indirection so a `tailwind-merge` pass can be added later without
 * touching call sites.
 */
export function cn(...inputs: ClassValue[]): string {
  return clsx(inputs);
}
