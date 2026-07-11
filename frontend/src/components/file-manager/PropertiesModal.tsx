import { useEffect, useState } from 'react';
import { X } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { Button } from '../ui/Button';
import { Input } from '../ui/Input';
import { Tooltip } from '../ui/Tooltip';
import { headObject, updateMetadata } from '../../lib/wails/fileManager';
import { formatBytes } from '../../lib/utils';
import { toast } from '../../lib/toast';
import { ApiError } from '../../lib/wails/errors';
import type { ObjectEntry, ObjectMeta } from '../../types';

export interface PropertiesModalProps {
  isOpen: boolean;
  onClose: () => void;
  profileId: number;
  bucket: string;
  entry: ObjectEntry;
}

interface MetadataPair {
  key: string;
  value: string;
}

function metaToPairs(metadata: Record<string, string>): MetadataPair[] {
  return Object.entries(metadata).map(([key, value]) => ({ key, value }));
}

/**
 * Object properties/metadata editor, per docs/03-ux-ui-spec.md section
 * 5.8.3.
 *
 * Fetches a fresh `ObjectMeta` via `headObject` on open (the `ObjectEntry`
 * passed in comes from the cached listing — size/contentType there can be
 * stale, and it never carries `etag`/user metadata at all). Deliberately has
 * no "Владелец" (owner) or storage-class row: `HeadObject` doesn't return
 * either, and a separate round-trip just for an owner display isn't
 * justified (documented Stage 4 scope limitation).
 *
 * `Content-Type`/`Cache-Control` are edited as two dedicated fields, kept
 * separate from the freeform metadata list below them: they're real HTTP
 * response headers, not `x-amz-meta-*` user metadata, matching
 * `UpdateMetadataRequest`'s own split between `ContentType`/`CacheControl`
 * and `UserMetadata`.
 */
export function PropertiesModal({ isOpen, onClose, profileId, bucket, entry }: PropertiesModalProps) {
  const [meta, setMeta] = useState<ObjectMeta | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [contentType, setContentType] = useState('');
  const [cacheControl, setCacheControl] = useState('');
  const [pairs, setPairs] = useState<MetadataPair[]>([]);

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;
    setIsLoading(true);
    setMeta(null);
    void headObject(profileId, bucket, entry.key)
      .then((result) => {
        if (cancelled) return;
        setMeta(result);
        setContentType(result.contentType);
        // No dedicated Cache-Control field on `ObjectMeta` — only
        // `UpdateMetadataRequest` accepts one, there's nothing to seed it
        // from on read, so it starts empty (S3 has no Cache-Control by
        // default either).
        setCacheControl('');
        setPairs(metaToPairs(result.metadata));
      })
      .catch((err) => {
        console.error('[PropertiesModal] headObject failed:', err);
        toast.error(
          err instanceof ApiError ? err.message : 'Не удалось загрузить свойства объекта',
          err instanceof ApiError ? err.raw : undefined,
        );
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, profileId, bucket, entry.key]);

  function updatePair(index: number, field: 'key' | 'value', value: string) {
    setPairs((prev) => prev.map((pair, i) => (i === index ? { ...pair, [field]: value } : pair)));
  }

  function removePair(index: number) {
    setPairs((prev) => prev.filter((_, i) => i !== index));
  }

  async function handleSave() {
    setIsSaving(true);
    try {
      const userMetadata: Record<string, string> = {};
      for (const pair of pairs) {
        if (pair.key.trim() === '') continue;
        userMetadata[pair.key.trim()] = pair.value;
      }
      await updateMetadata(profileId, bucket, entry.key, contentType, cacheControl, userMetadata);
      onClose();
    } catch (err) {
      console.error('[PropertiesModal] updateMetadata failed:', err);
      toast.error(
        err instanceof ApiError ? err.message : 'Не удалось сохранить метаданные',
        err instanceof ApiError ? err.raw : undefined,
      );
    } finally {
      setIsSaving(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Свойства объекта"
      size="large"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={isSaving}>
            Отмена
          </Button>
          <Button variant="primary" isLoading={isSaving} disabled={isLoading} onClick={() => void handleSave()}>
            Сохранить
          </Button>
        </>
      }
    >
      {isLoading || !meta ? (
        <p className="text-[13px] text-fg-muted">Загрузка…</p>
      ) : (
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-[120px_1fr] gap-x-3 gap-y-1.5 text-[13px]">
            <dt className="text-fg-muted">Имя</dt>
            <dd className="truncate text-fg-primary" title={meta.key}>
              {meta.key}
            </dd>
            <dt className="text-fg-muted">Размер</dt>
            <dd className="text-fg-primary">{formatBytes(meta.size)}</dd>
            <dt className="text-fg-muted">Тип</dt>
            <dd className="truncate text-fg-primary">{meta.contentType || '—'}</dd>
            <dt className="text-fg-muted">ETag</dt>
            <dd className="truncate text-fg-primary">{meta.etag || '—'}</dd>
            <dt className="text-fg-muted">Изменён</dt>
            <dd className="text-fg-primary">{meta.lastModified || '—'}</dd>
          </dl>

          <div className="flex flex-col gap-3 border-t border-border pt-3">
            <Input label="Content-Type" value={contentType} onChange={(event) => setContentType(event.target.value)} />
            <Input label="Cache-Control" value={cacheControl} onChange={(event) => setCacheControl(event.target.value)} />
          </div>

          <div className="flex flex-col gap-2 border-t border-border pt-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-fg-secondary">Метаданные</span>
              <Button
                variant="ghost"
                onClick={() => setPairs((prev) => [...prev, { key: '', value: '' }])}
              >
                + Добавить
              </Button>
            </div>
            {pairs.map((pair, index) => (
              // eslint-disable-next-line react/no-array-index-key
              <div key={index} className="flex items-center gap-2">
                <Input
                  placeholder="Ключ"
                  value={pair.key}
                  onChange={(event) => updatePair(index, 'key', event.target.value)}
                  className="flex-1"
                />
                <Input
                  placeholder="Значение"
                  value={pair.value}
                  onChange={(event) => updatePair(index, 'value', event.target.value)}
                  className="flex-1"
                />
                <Tooltip content="Удалить пару">
                  <Button iconOnly variant="ghost" aria-label="Удалить пару" onClick={() => removePair(index)}>
                    <X className="h-4 w-4" aria-hidden="true" />
                  </Button>
                </Tooltip>
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
}
