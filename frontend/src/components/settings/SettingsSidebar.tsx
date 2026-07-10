import { Info, Lock, Palette, SlidersHorizontal, Wifi, Wrench } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/utils';

export type SettingsSection = 'general' | 'appearance' | 'transfers' | 'security' | 'network' | 'about';

interface SectionItem {
  id: SettingsSection;
  label: string;
  icon: LucideIcon;
}

const SECTIONS: SectionItem[] = [
  { id: 'general', label: 'Общие', icon: Wrench },
  { id: 'appearance', label: 'Внешний вид', icon: Palette },
  { id: 'transfers', label: 'Передачи', icon: SlidersHorizontal },
  { id: 'security', label: 'Безопасность', icon: Lock },
  { id: 'network', label: 'Сетевые', icon: Wifi },
  { id: 'about', label: 'О приложении', icon: Info },
];

export interface SettingsSidebarProps {
  activeSection: SettingsSection;
  onSelectSection: (section: SettingsSection) => void;
}

/**
 * Settings sub-sidebar (200px), per docs/03-ux-ui-spec.md section 5.7 — the
 * same "main nav 240px + contextual sub-panel" pattern as `BucketPanel`
 * (Stage 2, Architectural Decision 6), applied to the Settings screen.
 */
export function SettingsSidebar({ activeSection, onSelectSection }: SettingsSidebarProps) {
  return (
    <aside className="flex h-full w-[200px] shrink-0 flex-col border-r border-border bg-bg-secondary py-2">
      {SECTIONS.map(({ id, label, icon: Icon }) => {
        const active = activeSection === id;
        return (
          <button
            key={id}
            type="button"
            onClick={() => onSelectSection(id)}
            className={cn(
              'flex h-9 items-center gap-2 border-l-2 px-3 text-left text-[13px] transition-colors duration-fast',
              active
                ? 'border-accent bg-accent-subtle text-accent'
                : 'border-transparent text-fg-secondary hover:bg-bg-tertiary',
            )}
          >
            <Icon className="h-4 w-4 shrink-0" aria-hidden="true" />
            <span className="truncate">{label}</span>
          </button>
        );
      })}
    </aside>
  );
}
