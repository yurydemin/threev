import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { getPresignedUrl } from '../../lib/wails/fileManager';
import { copyToClipboard } from '../../lib/utils';
import { toast } from '../../lib/toast';
import { ApiError } from '../../lib/wails/errors';

export interface PresignedUrlModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
  bucket: string;
  /** Named `objectKey`, not `key`, purely for readability at call sites — this isn't a React list `key`. */
  objectKey: string;
}

const MIN_EXPIRY_SECONDS = 60;
const MAX_EXPIRY_SECONDS = 604_800; // 7 days — matches the backend clamp (internal/filemanager/presign.go).
const DEFAULT_EXPIRY_SECONDS = 3600; // 1 hour.

/** Human-readable, not grammatically exhaustive, duration label (e.g. "5 мин.", "3 дн."). */
function formatDuration(seconds: number, t: TFunction): string {
  if (seconds < 3600) {
    const minutes = Math.round(seconds / 60);
    return t('units.minutesShort', { count: minutes });
  }
  if (seconds < 86_400) {
    const hours = Math.round(seconds / 3600);
    return t('units.hoursShort', { count: hours });
  }
  const days = Math.round(seconds / 86_400);
  return t('units.daysShort', { count: days });
}

/**
 * Presigned URL generator, per docs/03-ux-ui-spec.md sections 4.10/5.7/5.8.2.
 *
 * The expiry slider is a native `<input type="range">` — the project has no
 * dedicated Slider primitive yet (see `components/ui/*`), so this is styled
 * minimally rather than pixel-matched to the spec.
 */
export function PresignedUrlModal({ isOpen, onClose, profileId, bucket, objectKey }: PresignedUrlModalProps) {
  const { t } = useTranslation();
  const [expirySeconds, setExpirySeconds] = useState(DEFAULT_EXPIRY_SECONDS);
  const [url, setUrl] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;
    setIsLoading(true);
    void getPresignedUrl(profileId, bucket, objectKey, expirySeconds)
      .then((result) => {
        if (!cancelled) setUrl(result);
      })
      .catch((err) => {
        console.error('[PresignedUrlModal] getPresignedUrl failed:', err);
        toast.error(
          err instanceof ApiError ? err.message : t('fileManager.presignedUrlModal.genericError'),
          err instanceof ApiError ? err.raw : undefined,
        );
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, profileId, bucket, objectKey, expirySeconds]);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t('fileManager.presignedUrlModal.title')}
      footer={
        <Button variant="secondary" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-fg-secondary">{t('fileManager.presignedUrlModal.expiryLabel')}</span>
            <span className="text-xs text-fg-primary">{formatDuration(expirySeconds, t)}</span>
          </div>
          <input
            type="range"
            min={MIN_EXPIRY_SECONDS}
            max={MAX_EXPIRY_SECONDS}
            step={60}
            value={expirySeconds}
            onChange={(event) => setExpirySeconds(Number(event.target.value))}
            className="h-1 w-full cursor-pointer appearance-none rounded-full bg-bg-tertiary accent-accent"
          />
        </div>

        <div className="flex items-end gap-2">
          <Input
            label={t('fileManager.presignedUrlModal.urlLabel')}
            readOnly
            value={isLoading ? t('common.loading') : url}
            className="flex-1 font-mono text-xs"
          />
          <Button
            variant="primary"
            disabled={isLoading || !url}
            onClick={() => void copyToClipboard(url)}
          >
            {t('fileManager.presignedUrlModal.copy')}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
