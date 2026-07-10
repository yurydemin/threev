import { Clock } from 'lucide-react';

export interface PlaceholderSectionProps {
  title: string;
  description: string;
}

/**
 * "Coming soon" placeholder, shared by the "Безопасность" (master password
 * — Stage 4 Block H/I, no backend yet) and "Сетевые" (proxy settings —
 * backlog per the Stage 4 plan) sections, which have no real content yet.
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
