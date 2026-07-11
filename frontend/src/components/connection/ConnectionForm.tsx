import { useEffect, useState } from 'react';
import { AlertTriangle, Check, ChevronDown, ChevronRight, Eye, EyeOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { testConnection as apiTestConnection } from '../../lib/wails/connection';
import { useConnectionStore } from '../../stores/useConnectionStore';
import type { Connection, ConnectionFormValues } from '../../types';
import { Button } from '../ui/Button';
import { Checkbox } from '../ui/Checkbox';
import { Input } from '../ui/Input';
import { Modal } from '../ui/Modal';
import { Tooltip } from '../ui/Tooltip';

export interface ConnectionFormProps {
  isOpen: boolean;
  onClose: () => void;
  /** When set, the form edits this connection; otherwise it creates a new one. */
  initialValues?: Connection;
  onSaved: (connection: Connection) => void;
}

const EMPTY_VALUES: ConnectionFormValues = {
  name: '',
  endpointUrl: '',
  region: 'us-east-1',
  accessKeyId: '',
  secretAccessKey: '',
  sessionToken: '',
  pathStyle: false,
  verifySsl: true,
  customHeaders: {},
};

// S3-compatible providers accept arbitrary region strings (Yandex, MinIO,
// Backblaze, ...), so a closed `Select` would fight the user on anything
// outside AWS. A plain text input with a `<datalist>` of common values keeps
// free-form entry while still offering one-click suggestions — the more
// pragmatic reading of section 5.3's dropdown mockup for this domain.
const REGION_SUGGESTIONS = ['us-east-1', 'us-west-2', 'eu-west-1', 'eu-central-1', 'ru-central1', 'auto'];

type TestState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'success'; message: string; detail: string }
  | { status: 'error'; message: string; detail: string };

function valuesFromConnection(connection: Connection | undefined): ConnectionFormValues {
  if (!connection) return EMPTY_VALUES;
  return {
    name: connection.name,
    endpointUrl: connection.endpointUrl,
    region: connection.region,
    accessKeyId: connection.accessKeyId,
    secretAccessKey: connection.secretAccessKey,
    sessionToken: connection.sessionToken,
    pathStyle: connection.pathStyle,
    verifySsl: connection.verifySsl,
    customHeaders: connection.customHeaders,
  };
}

/**
 * Create/edit connection modal per docs/03-ux-ui-spec.md section 5.3.
 *
 * Custom headers (FR-CONN-002) are intentionally NOT editable here — the
 * form keeps whatever `customHeaders` it was loaded with (empty object for
 * new connections) and always round-trips it unchanged. Building an
 * add/remove key-value list is a reasonable follow-up but was cut to keep
 * Stage 1 scope tight; this is a conscious simplification, not an oversight.
 *
 * "Тестировать" and "Сохранить" are independent: testing never blocks
 * saving and vice versa, matching `ConnectionService`'s soft-validation
 * contract (a failed test does not prevent `SaveProfile`).
 */
export function ConnectionForm({ isOpen, onClose, initialValues, onSaved }: ConnectionFormProps) {
  const { t } = useTranslation();
  const isEditing = !!initialValues;
  const storeSaveConnection = useConnectionStore((state) => state.saveConnection);

  const [values, setValues] = useState<ConnectionFormValues>(EMPTY_VALUES);
  const [showSecret, setShowSecret] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [testState, setTestState] = useState<TestState>({ status: 'idle' });
  const [showTestDetail, setShowTestDetail] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Reset all transient form/UI state whenever the modal (re)opens, so
  // stale values/results from a previous session don't leak in.
  useEffect(() => {
    if (!isOpen) return;
    setValues(valuesFromConnection(initialValues));
    setShowSecret(false);
    setAdvancedOpen(false);
    setTestState({ status: 'idle' });
    setShowTestDetail(false);
    setIsSaving(false);
    setSaveError(null);
  }, [isOpen, initialValues]);

  function update<K extends keyof ConnectionFormValues>(key: K, value: ConnectionFormValues[K]) {
    setValues((prev) => ({ ...prev, [key]: value }));
  }

  const canSave =
    values.name.trim() !== '' &&
    values.endpointUrl.trim() !== '' &&
    values.accessKeyId.trim() !== '' &&
    (values.secretAccessKey.trim() !== '' || isEditing);

  function currentPayload() {
    return initialValues ? { ...values, id: initialValues.id } : values;
  }

  async function handleTest() {
    setTestState({ status: 'loading' });
    setShowTestDetail(false);
    try {
      const result = await apiTestConnection(currentPayload());
      setTestState(
        result.success
          ? { status: 'success', message: result.message, detail: result.detail }
          : { status: 'error', message: result.message, detail: result.detail },
      );
    } catch (err) {
      setTestState({
        status: 'error',
        message: err instanceof Error ? err.message : t('connections.form.testGenericError'),
        detail: '',
      });
    }
  }

  async function handleSave() {
    if (!canSave) return;
    setIsSaving(true);
    setSaveError(null);
    const saved = await storeSaveConnection(currentPayload());
    setIsSaving(false);
    if (saved) {
      onSaved(saved);
      onClose();
    } else {
      setSaveError(useConnectionStore.getState().error ?? t('connections.form.saveError'));
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={isEditing ? t('connections.form.titleEdit') : t('connections.form.titleNew')}
      footer={
        <div className="flex w-full flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <Button variant="secondary" onClick={handleTest} isLoading={testState.status === 'loading'}>
              {t('connections.form.test')}
            </Button>
            <div className="flex items-center gap-2">
              <Button variant="secondary" onClick={onClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" onClick={handleSave} disabled={!canSave} isLoading={isSaving}>
                {t('common.save')}
              </Button>
            </div>
          </div>

          {(testState.status === 'success' || testState.status === 'error') && (
            <div className="flex flex-col gap-1 text-xs">
              <div
                className={cn(
                  'flex items-center gap-1.5',
                  testState.status === 'success' ? 'text-success' : 'text-danger',
                )}
              >
                {testState.status === 'success' ? (
                  <Check className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                ) : (
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                )}
                <span>
                  {testState.message ||
                    (testState.status === 'success' ? t('connections.form.testSuccess') : t('connections.form.testError'))}
                </span>
              </div>
              {testState.detail && (
                <button
                  type="button"
                  onClick={() => setShowTestDetail((prev) => !prev)}
                  className="self-start text-fg-muted hover:text-fg-secondary hover:underline"
                >
                  {showTestDetail ? t('connections.form.hideTechnicalDetails') : t('connections.form.showTechnicalDetails')}
                </button>
              )}
              {showTestDetail && testState.detail && (
                <p className="whitespace-pre-wrap break-all font-mono text-[11px] text-fg-muted">
                  {testState.detail}
                </p>
              )}
            </div>
          )}

          {saveError && <p className="text-xs text-danger">{saveError}</p>}
        </div>
      }
    >
      <div className="flex flex-col gap-3">
        <Input
          label={t('connections.form.nameLabel')}
          value={values.name}
          onChange={(e) => update('name', e.target.value)}
          placeholder={t('connections.form.namePlaceholder')}
          required
        />

        <Input
          label={t('connections.form.endpointLabel')}
          value={values.endpointUrl}
          onChange={(e) => update('endpointUrl', e.target.value)}
          placeholder={t('connections.form.endpointPlaceholder')}
          required
        />

        <div className="flex flex-col">
          <label htmlFor="connection-region" className="mb-1 text-xs font-medium text-fg-secondary">
            {t('connections.form.regionLabel')}
          </label>
          <input
            id="connection-region"
            list="connection-region-suggestions"
            value={values.region}
            onChange={(e) => update('region', e.target.value)}
            placeholder={t('connections.form.regionPlaceholder')}
            className={cn(
              'h-8 w-full rounded border border-border bg-bg-secondary px-2.5 text-[13px] text-fg-primary',
              'placeholder:text-fg-muted transition-colors duration-fast',
              'focus:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-subtle',
            )}
          />
          <datalist id="connection-region-suggestions">
            {REGION_SUGGESTIONS.map((region) => (
              <option key={region} value={region} />
            ))}
          </datalist>
        </div>

        <Input
          label={t('connections.form.accessKeyLabel')}
          value={values.accessKeyId}
          onChange={(e) => update('accessKeyId', e.target.value)}
          required
        />

        <div className="flex flex-col">
          <label htmlFor="connection-secret" className="mb-1 text-xs font-medium text-fg-secondary">
            {t('connections.form.secretKeyLabel')}
          </label>
          <div className="relative">
            <input
              id="connection-secret"
              type={showSecret ? 'text' : 'password'}
              value={values.secretAccessKey}
              onChange={(e) => update('secretAccessKey', e.target.value)}
              className={cn(
                'h-8 w-full rounded border border-border bg-bg-secondary px-2.5 pr-9 text-[13px] text-fg-primary',
                'placeholder:text-fg-muted transition-colors duration-fast',
                'focus:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-subtle',
              )}
            />
            <Tooltip content={showSecret ? t('connections.form.hideSecret') : t('connections.form.showSecret')}>
              <button
                type="button"
                onClick={() => setShowSecret((prev) => !prev)}
                aria-label={showSecret ? t('connections.form.hideSecret') : t('connections.form.showSecret')}
                className="absolute right-1 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-sm text-fg-secondary transition-colors duration-fast hover:bg-bg-tertiary"
              >
                {showSecret ? (
                  <EyeOff className="h-4 w-4" aria-hidden="true" />
                ) : (
                  <Eye className="h-4 w-4" aria-hidden="true" />
                )}
              </button>
            </Tooltip>
          </div>
        </div>

        <Input
          label={t('connections.form.sessionTokenLabel')}
          value={values.sessionToken}
          onChange={(e) => update('sessionToken', e.target.value)}
          placeholder={t('connections.form.sessionTokenPlaceholder')}
        />

        <div className="border-t border-border pt-2">
          <button
            type="button"
            onClick={() => setAdvancedOpen((prev) => !prev)}
            className="flex w-full items-center gap-1.5 py-1 text-left text-xs font-semibold uppercase tracking-wide text-fg-secondary"
            aria-expanded={advancedOpen}
          >
            {advancedOpen ? (
              <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {t('connections.form.advanced')}
          </button>
          <div
            className={cn(
              'overflow-hidden transition-[max-height] duration-normal ease-out',
              advancedOpen ? 'max-h-96' : 'max-h-0',
            )}
          >
            <div className="flex flex-col gap-2 pb-1 pt-2">
              <Checkbox
                label={t('connections.form.pathStyleLabel')}
                checked={values.pathStyle}
                onChange={(e) => update('pathStyle', e.target.checked)}
              />
              <Checkbox
                label={t('connections.form.verifySslLabel')}
                checked={values.verifySsl}
                onChange={(e) => update('verifySsl', e.target.checked)}
              />
            </div>
          </div>
        </div>
      </div>
    </Modal>
  );
}
