import { Info, Lock, Palette, SlidersHorizontal, Wifi, Wrench } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { cn } from '../../lib/utils';

export type SettingsSection = 'general' | 'appearance' | 'transfers' | 'security' | 'network' | 'about';

interface SectionItem {
  id: SettingsSection;
  label: string;
  icon: LucideIcon;
}

function getSections(t: TFunction): SectionItem[] {
  return [
    { id: 'general', label: t('settings.sidebar.general'), icon: Wrench },
    { id: 'appearance', label: t('settings.sidebar.appearance'), icon: Palette },
    { id: 'transfers', label: t('settings.sidebar.transfers'), icon: SlidersHorizontal },
    { id: 'security', label: t('settings.sidebar.security'), icon: Lock },
    { id: 'network', label: t('settings.sidebar.network'), icon: Wifi },
    { id: 'about', label: t('settings.sidebar.about'), icon: Info },
  ];
}

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
  const { t } = useTranslation();
  const SECTIONS = getSections(t);
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
