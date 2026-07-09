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
  readonly raw: string;

  constructor(raw: string) {
    super(raw);
    this.name = 'ApiError';
    this.raw = raw;
  }
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
