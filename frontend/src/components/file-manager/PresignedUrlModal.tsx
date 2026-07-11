import { useEffect, useState } from 'react';
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

/** Human-readable, not grammatically exhaustive, duration label (e.g. "5 минут", "3 дня"). */
function formatDuration(seconds: number): string {
  if (seconds < 3600) {
    const minutes = Math.round(seconds / 60);
    return `${minutes} мин.`;
  }
  if (seconds < 86_400) {
    const hours = Math.round(seconds / 3600);
    return `${hours} ч.`;
  }
  const days = Math.round(seconds / 86_400);
  return `${days} дн.`;
}

/**
 * Presigned URL generator, per docs/03-ux-ui-spec.md sections 4.10/5.7/5.8.2.
 *
 * The expiry slider is a native `<input type="range">` — the project has no
 * dedicated Slider primitive yet (see `components/ui/*`), so this is styled
 * minimally rather than pixel-matched to the spec.
 */
export function PresignedUrlModal({ isOpen, onClose, profileId, bucket, objectKey }: PresignedUrlModalProps) {
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
          err instanceof ApiError ? err.message : 'Не удалось получить presigned URL',
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
      title="Получить presigned URL"
      footer={
        <Button variant="secondary" onClick={onClose}>
          Закрыть
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-fg-secondary">Срок действия</span>
            <span className="text-xs text-fg-primary">{formatDuration(expirySeconds)}</span>
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
            label="URL"
            readOnly
            value={isLoading ? 'Загрузка…' : url}
            className="flex-1 font-mono text-xs"
          />
          <Button
            variant="primary"
            disabled={isLoading || !url}
            onClick={() => void copyToClipboard(url)}
          >
            Копировать
          </Button>
        </div>
      </div>
    </Modal>
  );
}
