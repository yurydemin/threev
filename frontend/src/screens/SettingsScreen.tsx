import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { Sidebar } from '../components/layout/Sidebar';
import { StatusBar } from '../components/layout/StatusBar';
import { SettingsSidebar, type SettingsSection } from '../components/settings/SettingsSidebar';
import { GeneralSection } from '../components/settings/GeneralSection';
import { AppearanceSection } from '../components/settings/AppearanceSection';
import { TransfersSection } from '../components/settings/TransfersSection';
import { SecuritySection } from '../components/settings/SecuritySection';
import { PlaceholderSection } from '../components/settings/PlaceholderSection';
import { AboutSection } from '../components/settings/AboutSection';
import { Button } from '../components/ui/Button';
import { useSettingsStore } from '../stores/useSettingsStore';
import type { AppSettings } from '../types';

function getSectionTitles(t: TFunction): Record<SettingsSection, string> {
  return {
    general: t('settings.screen.sections.general'),
    appearance: t('settings.screen.sections.appearance'),
    transfers: t('settings.screen.sections.transfers'),
    security: t('settings.screen.sections.security'),
    network: t('settings.screen.sections.network'),
    about: t('settings.screen.sections.about'),
  };
}

export interface SettingsScreenProps {
  /** Navigates to the Connections screen (Sidebar "Подключения"). */
  onSelectConnections: () => void;
  /** Navigates to the Transfers screen (Sidebar "Передачи"). */
  onSelectTransfers: () => void;
  /** Navigates to the History screen (Sidebar "История"). */
  onSelectHistory: () => void;
  /** Returns to an already-open File Manager session (Sidebar active-connection indicator, Block L2). */
  onSelectFileManager: () => void;
  /** Closes the open File Manager session (Sidebar active-connection indicator's "X" button). */
  onDisconnect: () => void;
}

/**
 * "Настройки" screen per docs/03-ux-ui-spec.md section 5.7 — the same
 * "main Sidebar (240px) + narrow contextual sub-panel (200px) + content"
 * structural pattern as `FileManagerScreen`'s `BucketPanel`
 * (Architectural Decision 6, Stage 2), with `SettingsSidebar` standing in
 * for `BucketPanel`.
 *
 * Doesn't call `fetchSettings` itself — `useSettingsSync` (mounted once at
 * the `App.tsx` root, same placement as `useTransferEvents`) already
 * hydrates `useSettingsStore`, since theme/UI-scale reconciliation is
 * needed from app startup, not just while this screen is open.
 *
 * `draft` is a local copy of `settings`, edited by each section via
 * `onChange`, and only written back with the single screen-wide "Сохранить
 * изменения" button (per the UX mockup — no per-section save buttons). The
 * `draft === null` guard in the sync effect below means it is seeded from
 * `settings` exactly once: if `fetchSettings()` ever resolves again later
 * (e.g. triggered elsewhere while this screen is mounted), it updates
 * `useSettingsStore`'s `settings` but does NOT clobber whatever unsaved
 * edits the user already made in `draft`.
 */
export function SettingsScreen({
  onSelectConnections,
  onSelectTransfers,
  onSelectHistory,
  onSelectFileManager,
  onDisconnect,
}: SettingsScreenProps) {
  const { t } = useTranslation();
  const SECTION_TITLES = getSectionTitles(t);
  const [section, setSection] = useState<SettingsSection>('general');
  const settings = useSettingsStore((state) => state.settings);
  const [draft, setDraft] = useState<AppSettings | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    if (draft === null && settings !== null) {
      setDraft(settings);
    }
  }, [draft, settings]);

  function updateDraft(patch: Partial<AppSettings>) {
    setDraft((prev) => (prev ? { ...prev, ...patch } : prev));
  }

  async function handleSave() {
    if (!draft) return;
    setIsSaving(true);
    await useSettingsStore.getState().saveSettings(draft);
    setIsSaving(false);
  }

  return (
    <div className="flex h-screen w-full">
      <Sidebar
        activeItem="settings"
        onSelectConnections={onSelectConnections}
        onSelectTransfers={onSelectTransfers}
        onSelectHistory={onSelectHistory}
        onSelectFileManager={onSelectFileManager}
        onDisconnect={onDisconnect}
      />
      <SettingsSidebar activeSection={section} onSelectSection={setSection} />

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-header shrink-0 items-center justify-between border-b border-border bg-bg-secondary px-6">
          <h1 className="text-[18px] font-semibold text-fg-primary">{SECTION_TITLES[section]}</h1>
          <Button variant="primary" onClick={handleSave} disabled={!draft} isLoading={isSaving}>
            {t('settings.screen.saveButton')}
          </Button>
        </header>

        <main className="flex-1 overflow-y-auto px-6 py-4">
          {draft === null ? (
            <p className="text-sm text-fg-muted">{t('common.loading')}</p>
          ) : section === 'general' ? (
            <GeneralSection value={draft} onChange={updateDraft} />
          ) : section === 'appearance' ? (
            <AppearanceSection value={draft} onChange={updateDraft} />
          ) : section === 'transfers' ? (
            <TransfersSection value={draft} onChange={updateDraft} />
          ) : section === 'security' ? (
            <SecuritySection />
          ) : section === 'network' ? (
            <PlaceholderSection
              title={t('settings.screen.networkPlaceholderTitle')}
              description={t('settings.screen.networkPlaceholderDescription')}
            />
          ) : (
            <AboutSection />
          )}
        </main>

        <StatusBar />
      </div>
    </div>
  );
}
