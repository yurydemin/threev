import type { ReactNode } from 'react';

export interface SettingFieldProps {
  label: string;
  description?: string;
  children: ReactNode;
}

/**
 * One labeled setting control, per docs/03-ux-ui-spec.md section 5.7: label
 * 13px/500 `--fg-secondary` above the control, optional 12px `--fg-muted`
 * description below it. Shared by every settings section.
 */
export function SettingField({ label, description, children }: SettingFieldProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-[13px] font-medium text-fg-secondary">{label}</span>
      {children}
      {description && <p className="text-xs text-fg-muted">{description}</p>}
    </div>
  );
}
