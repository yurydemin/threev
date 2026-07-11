import i18n from '../../i18n';

/**
 * Shared error-handling infrastructure for typed wrappers around the
 * generated `wailsjs/go/**` bindings.
 *
 * Wails surfaces Go errors as rejected promises whose rejection value is
 * either a plain string or an `Error`. `ApiError` normalizes that into a
 * single shape the rest of the app can rely on, and `call` wraps a binding
 * invocation so callers don't have to repeat try/catch boilerplate.
 *
 * A single `ApiError` class is shared across domains (connection,
 * fileManager, ...) rather than one subclass per domain: nothing in the app
 * currently branches on the error's domain, only on its `raw`/`message`
 * content, so per-domain subclasses would add ceremony without value. If a
 * call site ever needs to distinguish origins, `ApiError` can be extended
 * with an optional `source` field instead of multiplying subclasses.
 */

/** Uniform error shape for failures coming out of the Go backend via Wails. */
export class ApiError extends Error {
  /** The original, technical error text from the Go backend — surfaced via toast's "Скопировать детали" (UX-007). */
  readonly raw: string;

  constructor(raw: string) {
    super(friendlyMessage(raw));
    this.name = 'ApiError';
    this.raw = raw;
  }
}

/**
 * Maps common raw Go backend error strings to user-facing Russian messages
 * (UX-007: "пользовательские сообщения вместо технических stack trace").
 *
 * Two kinds of source text show up in `raw`:
 *  - Already-Russian phrases the backend itself produces for classified S3/
 *    network failures (`internal/s3client/errors.go#ClassifyError`,
 *    `internal/filemanager/errors.go#classifyOperationError`) — e.g. "Бакет
 *    не найден", "Неверные учётные данные" — but wrapped in an
 *    `"<operation>: <message> (<category>): <original error>"` envelope
 *    that's too noisy to show as-is. These patterns unwrap them.
 *  - Plain Go sentinel/validation error text with no such wrapping (e.g.
 *    `domain.ErrLocked`'s "application is locked", or raw `net`/context
 *    errors from paths `ClassifyError` never sees) — matched directly.
 *
 * Falls back to the raw message unchanged when nothing matches — a
 * technical-but-honest message beats inventing a generic, unhelpful "что-то
 * пошло не так" for an error this function doesn't recognize.
 */
function friendlyMessage(raw: string): string {
  const patterns: [RegExp, string][] = [
    // Already-Russian, backend-classified S3/network failures — unwrap the
    // "<op>: <message> (<category>): <raw>" envelope down to just <message>.
    [/Бакет не найден/i, i18n.t('errors.bucketNotFound')],
    [/Объект не найден/i, i18n.t('errors.objectNotFound')],
    [/Неверные учётные данные/i, i18n.t('errors.invalidCredentials')],
    [/Превышено время ожидания подключения/i, i18n.t('errors.timeout')],
    [/Ошибка проверки SSL-сертификата/i, i18n.t('errors.sslError')],
    [/Не удалось подключиться к endpoint/i, i18n.t('errors.connectionFailed')],

    // Raw Go/network errors from paths that don't go through ClassifyError.
    [/connection refused|no such host|dial tcp/i, i18n.t('errors.connectionFailed')],
    [/context deadline exceeded|i\/o timeout/i, i18n.t('errors.timeout')],
    [/circuit breaker open|temporarily unavailable/i, i18n.t('errors.serverUnavailable')],
    [/(invalidaccesskeyid|signaturedoesnotmatch|access ?denied)/i, i18n.t('errors.invalidCredentials')],
    [/nosuchbucket/i, i18n.t('errors.bucketNotFound')],
    [/nosuchkey/i, i18n.t('errors.objectNotFound')],

    // `domain.*` sentinel errors (internal/domain/errors.go) and other
    // plain Go validation text.
    [/application is locked/i, i18n.t('errors.appLocked')],
    [/profile not found/i, i18n.t('errors.profileNotFound')],
    [/a profile with this name already exists/i, i18n.t('errors.profileNameExists')],
    [/invalid endpoint url/i, i18n.t('errors.invalidEndpoint')],
    [/profile name must not be empty/i, i18n.t('errors.profileNameEmpty')],
    [/incorrect password/i, i18n.t('errors.incorrectPassword')],
    [/bulk operation \d+ not found or already finished/i, i18n.t('errors.operationNotFound')],
    [/rename object: new key must not be empty/i, i18n.t('errors.newNameEmpty')],
    [/rename object: new key .* is the same as the current key/i, i18n.t('errors.newNameSame')],
    [/create folder: name must not be empty/i, i18n.t('errors.folderNameEmpty')],
    [/create folder: name .* must not contain/i, i18n.t('errors.folderNameSlash')],
    [/no keys given/i, i18n.t('errors.noKeysGiven')],
  ];

  for (const [pattern, message] of patterns) {
    if (pattern.test(raw)) return message;
  }

  return raw;
}

function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err;
  if (err instanceof Error) return new ApiError(err.message);
  if (typeof err === 'string') return new ApiError(err);
  return new ApiError('Unknown error');
}

/** Invokes `fn`, normalizing any rejection into an `ApiError`. */
export async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (err) {
    throw toApiError(err);
  }
}

/**
 * Converts a Wails-serialized Go `time.Time` value (or any Date-ish value)
 * into an ISO-8601 string. Falls back to an empty string for nullish input.
 */
export function toIsoString(value: unknown): string {
  if (!value) return '';
  if (typeof value === 'string') return value;
  if (value instanceof Date) return value.toISOString();
  return String(value);
}
