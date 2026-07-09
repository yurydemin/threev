import type { ReactNode } from 'react';

export interface StatusBarProps {
  /** e.g. "N объектов • X total". */
  left?: ReactNode;
  /** Transfer indicators — Stage 3. */
  right?: ReactNode;
}

/**
 * Status bar per docs/03-ux-ui-spec.md section 5.4.6, trimmed to what Stage
 * 2 needs (object/size counts on the left). Transfer indicators (right side)
 * land in Stage 3 — `right` is accepted now so the slot doesn't need to be
 * threaded through again later.
 */
export function StatusBar({ left, right }: StatusBarProps) {
  return (
    <div className="flex h-7 shrink-0 items-center justify-between border-t border-border bg-bg-primary px-4 text-2xs text-fg-secondary">
      <div className="truncate">{left}</div>
      <div className="shrink-0">{right}</div>
    </div>
  );
}
