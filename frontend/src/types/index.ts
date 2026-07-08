/**
 * Frontend-domain types mirroring Go DTOs exposed by `ConnectionService`
 * (see `wailsjs/go/models.ts`, namespace `domain`).
 *
 * These are plain TS interfaces rather than re-exports of the wailsjs
 * classes: the generated classes carry extra runtime machinery
 * (`convertValues`, `createFrom`) that UI code has no business depending on.
 *
 * Naming follows the frontend domain convention ("connection"), while the
 * Go backend keeps its own naming ("Profile"/"ProfileDTO") — see
 * constraint #12 in the Stage 1 plan.
 */

/** Full connection record, mirrors `domain.Profile` (includes secrets). */
export interface Connection {
  id: number;
  name: string;
  endpointUrl: string;
  region: string;
  accessKeyId: string;
  secretAccessKey: string;
  sessionToken: string;
  pathStyle: boolean;
  verifySsl: boolean;
  customHeaders: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

/** Lightweight connection summary, mirrors `domain.ProfileDTO` (no secrets). */
export interface ConnectionSummary {
  id: number;
  name: string;
  endpointUrl: string;
  region: string;
  pathStyle: boolean;
  verifySsl: boolean;
  createdAt: string;
  updatedAt: string;
}

/** Mirrors `domain.ConnectionTestResult`. */
export interface ConnectionTestResult {
  success: boolean;
  message: string;
  detail: string;
  category: string;
}

/**
 * The subset of `Connection` fields actually edited in the connection
 * form. `id`/`createdAt`/`updatedAt` are managed separately (assigned by
 * the backend / the store), not by form state.
 */
export type ConnectionFormValues = Omit<Connection, 'id' | 'createdAt' | 'updatedAt'>;
