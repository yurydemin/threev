import { Clock } from 'lucide-react';

export interface PlaceholderSectionProps {
  title: string;
  description: string;
}

/**
 * "Coming soon" placeholder. Used by the "Сетевые" (proxy settings —
 * backlog per the Stage 4 plan) section, which has no real content yet.
 * "Безопасность" moved off this placeholder onto `SecuritySection` in
 * Stage 4 Block I.
 */
export function PlaceholderSection({ title, description }: PlaceholderSectionProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
      <Clock className="h-8 w-8 text-fg-muted" aria-hidden="true" />
      <p className="text-[13px] font-medium text-fg-secondary">{title}</p>
      <p className="max-w-sm text-xs text-fg-muted">{description}</p>
    </div>
  );
}
