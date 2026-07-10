import type { ReactNode } from 'react';

export interface SettingGroupProps {
  children: ReactNode;
}

/**
 * Wraps a set of `SettingField`s, separated from the next group by
 * `--border-subtle`, per docs/03-ux-ui-spec.md section 5.7. The last group
 * in a section drops its own separator (`[&:last-child]`) so the section
 * doesn't end on a stray rule.
 */
export function SettingGroup({ children }: SettingGroupProps) {
  return (
    <div className="flex flex-col gap-4 border-b border-border-subtle py-4 first:pt-0 last:border-b-0 last:pb-0">
      {children}
    </div>
  );
}
