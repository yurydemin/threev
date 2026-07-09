/**
 * Preview-type classification, shared between `ObjectContextMenu` (decides
 * whether to show "Открыть / Предпросмотр" at all) and `ObjectPreviewModal`
 * (dispatches to the right renderer). One source of truth for "what counts
 * as previewable" per FR-FM-007 / Stage 2 constraint 6 — image, PDF, and
 * text — so the two components can never disagree about a given object.
 *
 * `contentType` here is always the value already carried on `ObjectEntry`
 * (from `ListObjects`, backed by `internal/filemanager/mime.go`'s static
 * extension table — see that file for why it's not the stdlib `mime`
 * package). The non-`text/*` entries below mirror the non-`text/*` MIME
 * types that table actually produces for text/code files (`json`, `xml`,
 * `yaml`, `sh`, `sql`), plus a couple of common aliases in case a future
 * caller ever feeds this a real S3-reported Content-Type instead.
 */

export type PreviewKind = 'image' | 'pdf' | 'text';

const TEXT_LIKE_CONTENT_TYPES = new Set([
  'application/json',
  'application/xml',
  'application/yaml',
  'application/x-yaml',
  'application/javascript',
  'application/x-javascript',
  'application/x-sh',
  'application/sql',
]);

/** Returns which preview renderer applies to `contentType`, or `null` if none does. */
export function getPreviewKind(contentType: string): PreviewKind | null {
  if (!contentType) return null;
  if (contentType.startsWith('image/')) return 'image';
  if (contentType === 'application/pdf') return 'pdf';
  if (contentType.startsWith('text/') || TEXT_LIKE_CONTENT_TYPES.has(contentType)) return 'text';
  return null;
}

/** Whether any preview renderer applies to `contentType` (Stage 2 constraint 6). */
export function isPreviewSupported(contentType: string): boolean {
  return getPreviewKind(contentType) !== null;
}
