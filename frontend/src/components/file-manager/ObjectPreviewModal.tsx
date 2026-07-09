import { useEffect, useState } from 'react';
import { Modal } from '../ui/Modal';
import { getPresignedUrl, getTextPreview } from '../../lib/wails/fileManager';
import { getPreviewKind } from '../../lib/preview';
import { formatBytes, getEntryDisplayName } from '../../lib/utils';
import { ApiError } from '../../lib/wails/errors';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import type { ObjectEntry, TextPreviewResult } from '../../types';

/** Same fixed TTL as "Копировать URL" — Stage 2 constraint 2. */
const PREVIEW_URL_EXPIRY_SECONDS = 300;

export interface ObjectPreviewModalProps {
  entry: ObjectEntry | null;
  isOpen: boolean;
  onClose: () => void;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Не удалось загрузить предпросмотр';
}

/**
 * Preview dispatcher per docs/03-ux-ui-spec.md section 5.4.5 / FR-FM-007 —
 * image and PDF open a presigned URL directly (`<img>`/`<iframe>`, per
 * Stage 2 Architectural Decisions), text goes through the dedicated
 * `GetTextPreview` backend call (size-limited, presigned URLs can't be).
 * Anything else never gets here: `ObjectContextMenu` only offers "Открыть /
 * Предпросмотр" for types `lib/preview.ts#getPreviewKind` recognizes, and
 * `FileManagerScreen`'s double-click handler applies the same check.
 *
 * Reads `activeProfileId`/`selectedBucket`/`currentPrefix` straight from
 * `useFileManagerStore` rather than taking them as props — same convention
 * as `Toolbar`/`BucketPanel`/`ObjectContextMenu` — since this modal never
 * makes sense to render outside the currently browsed bucket anyway.
 */
export function ObjectPreviewModal({ entry, isOpen, onClose }: ObjectPreviewModalProps) {
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const selectedBucket = useFileManagerStore((state) => state.selectedBucket);
  const currentPrefix = useFileManagerStore((state) => state.currentPrefix);

  const previewKind = entry ? getPreviewKind(entry.contentType) : null;

  const [presignedUrl, setPresignedUrl] = useState<string | null>(null);
  const [textResult, setTextResult] = useState<TextPreviewResult | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setPresignedUrl(null);
    setTextResult(null);
    setError(null);

    if (!entry || !previewKind || !activeProfileId || !selectedBucket) {
      setIsLoading(false);
      return;
    }

    let cancelled = false;
    setIsLoading(true);

    async function load() {
      try {
        if (previewKind === 'text') {
          const result = await getTextPreview(activeProfileId as number, selectedBucket as string, entry!.key);
          if (!cancelled) setTextResult(result);
        } else {
          const url = await getPresignedUrl(
            activeProfileId as number,
            selectedBucket as string,
            entry!.key,
            PREVIEW_URL_EXPIRY_SECONDS,
          );
          if (!cancelled) setPresignedUrl(url);
        }
      } catch (err) {
        if (!cancelled) setError(errorMessage(err));
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
    // `entry` (not just `entry?.key`) is intentional: a new object opened
    // while the modal is already visible must re-trigger the fetch even if,
    // in principle, no two distinct entries share a key.
  }, [entry, previewKind, activeProfileId, selectedBucket]);

  const title = entry ? getEntryDisplayName(entry.key, currentPrefix) : 'Предпросмотр';

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={title} size="preview">
      {!entry ? null : isLoading ? (
        <div className="flex h-full items-center justify-center text-sm text-fg-muted">
          Загрузка предпросмотра…
        </div>
      ) : error ? (
        <p className="p-4 text-center text-sm text-danger">{error}</p>
      ) : previewKind === 'image' && presignedUrl ? (
        <div className="flex h-full items-center justify-center">
          <img src={presignedUrl} alt={title} className="mx-auto max-h-full max-w-full object-contain" />
        </div>
      ) : previewKind === 'pdf' && presignedUrl ? (
        <iframe src={presignedUrl} title={title} className="h-full w-full border-0" />
      ) : previewKind === 'text' && textResult ? (
        <div>
          {textResult.truncated && (
            <p className="mb-2 text-xs text-fg-muted">
              Показаны первые 100 КБ из {formatBytes(textResult.totalSize)}
            </p>
          )}
          <pre className="overflow-auto whitespace-pre-wrap font-mono text-xs text-fg-primary">
            {textResult.content}
          </pre>
        </div>
      ) : (
        <p className="p-4 text-center text-sm text-fg-muted">Предпросмотр недоступен для этого типа файла.</p>
      )}
    </Modal>
  );
}
