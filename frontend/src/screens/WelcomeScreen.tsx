import { useState } from 'react';
import { CloudUpload } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ConnectionForm } from '../components/connection/ConnectionForm';
import { Button } from '../components/ui/Button';

/**
 * "Приветствие / Нет подключений" per docs/03-ux-ui-spec.md section 5.1.
 *
 * The `.env` drag-and-drop zone shown in the mockup is explicitly deferred
 * (see Stage 1 plan constraint #12) — rather than leaving a disabled/inert
 * drop zone that visually promises a feature this build doesn't implement,
 * it is omitted entirely.
 *
 * Owns its own `ConnectionForm` instance/open-state: once a connection is
 * saved, `useConnectionStore.saveConnection` refetches the list itself, so
 * `App` swaps this screen out for `ConnectionsScreen` automatically — no
 * callback needs to be threaded back up.
 */
export function WelcomeScreen() {
  const { t } = useTranslation();
  const [isFormOpen, setIsFormOpen] = useState(false);

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center">
      <CloudUpload className="h-12 w-12 text-accent" aria-hidden="true" />
      <div className="flex flex-col gap-1.5">
        <h1 className="text-xl font-semibold text-fg-primary">{t('welcome.title')}</h1>
        <p className="text-sm text-fg-secondary">
          {t('welcome.subtitle')}
        </p>
      </div>
      <Button variant="primary" size="large" onClick={() => setIsFormOpen(true)}>
        {t('welcome.addConnection')}
      </Button>

      <ConnectionForm
        isOpen={isFormOpen}
        onClose={() => setIsFormOpen(false)}
        onSaved={() => {}}
      />
    </div>
  );
}
