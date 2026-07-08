/**
 * Typed wrapper around the generated `wailsjs/go/connection/ConnectionService`
 * bindings.
 *
 * Responsibilities:
 * - Convert between the frontend-domain types (`types/index.ts`, camelCase)
 *   and the wailsjs-generated Go DTO classes (`domain.*`, PascalCase).
 * - Normalize rejected promises (Wails surfaces Go errors as string
 *   rejections) into a single `ConnectionApiError` shape the rest of the
 *   app can rely on.
 *
 * Do not import `wailsjs/go/**` anywhere else in the app — go through this
 * module instead.
 */
import {
  DeleteProfile,
  GetProfile,
  GetProfiles,
  SaveProfile,
  TestConnection as TestConnectionBinding,
} from '../../wailsjs/go/connection/ConnectionService';
import { domain } from '../../wailsjs/go/models';
import type {
  Connection,
  ConnectionFormValues,
  ConnectionSummary,
  ConnectionTestResult,
} from '../types';

/**
 * Uniform error shape for failures coming out of the Go backend via Wails.
 * `raw` preserves the original rejection value/message for debugging.
 */
export class ConnectionApiError extends Error {
  readonly raw: string;

  constructor(raw: string) {
    super(raw);
    this.name = 'ConnectionApiError';
    this.raw = raw;
  }
}

function toApiError(err: unknown): ConnectionApiError {
  if (err instanceof ConnectionApiError) return err;
  if (err instanceof Error) return new ConnectionApiError(err.message);
  if (typeof err === 'string') return new ConnectionApiError(err);
  return new ConnectionApiError('Unknown error');
}

async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (err) {
    throw toApiError(err);
  }
}

function toIsoString(value: unknown): string {
  if (!value) return '';
  if (typeof value === 'string') return value;
  if (value instanceof Date) return value.toISOString();
  return String(value);
}

function fromProfileDTO(dto: domain.ProfileDTO): ConnectionSummary {
  return {
    id: dto.ID,
    name: dto.Name,
    endpointUrl: dto.EndpointURL,
    region: dto.Region,
    pathStyle: dto.PathStyle,
    verifySsl: dto.VerifySSL,
    createdAt: toIsoString(dto.CreatedAt),
    updatedAt: toIsoString(dto.UpdatedAt),
  };
}

function fromProfile(profile: domain.Profile): Connection {
  return {
    id: profile.ID,
    name: profile.Name,
    endpointUrl: profile.EndpointURL,
    region: profile.Region,
    accessKeyId: profile.AccessKeyID,
    secretAccessKey: profile.SecretAccessKey,
    sessionToken: profile.SessionToken,
    pathStyle: profile.PathStyle,
    verifySsl: profile.VerifySSL,
    customHeaders: profile.CustomHeaders ?? {},
    createdAt: toIsoString(profile.CreatedAt),
    updatedAt: toIsoString(profile.UpdatedAt),
  };
}

function toProfile(connection: Partial<Connection> & ConnectionFormValues): domain.Profile {
  return domain.Profile.createFrom({
    ID: connection.id ?? 0,
    Name: connection.name,
    EndpointURL: connection.endpointUrl,
    Region: connection.region,
    AccessKeyID: connection.accessKeyId,
    SecretAccessKey: connection.secretAccessKey,
    SessionToken: connection.sessionToken,
    PathStyle: connection.pathStyle,
    VerifySSL: connection.verifySsl,
    CustomHeaders: connection.customHeaders ?? {},
  });
}

function fromConnectionTestResult(result: domain.ConnectionTestResult): ConnectionTestResult {
  return {
    success: result.Success,
    message: result.Message,
    detail: result.Detail,
    category: result.Category,
  };
}

export async function listConnections(): Promise<ConnectionSummary[]> {
  return call(async () => (await GetProfiles()).map(fromProfileDTO));
}

export async function getConnection(id: number): Promise<Connection> {
  return call(async () => fromProfile(await GetProfile(id)));
}

/**
 * Creates a new connection when `connection.id` is absent/`0`, otherwise
 * updates the existing one. Mirrors `ConnectionService.SaveProfile`
 * semantics (soft validation — network/credential issues do not block save).
 */
export async function saveConnection(
  connection: (Partial<Connection> & ConnectionFormValues) | Connection,
): Promise<Connection> {
  return call(async () => fromProfile(await SaveProfile(toProfile(connection))));
}

export async function deleteConnection(id: number): Promise<void> {
  return call(() => DeleteProfile(id));
}

export async function testConnection(
  connection: (Partial<Connection> & ConnectionFormValues) | Connection,
): Promise<ConnectionTestResult> {
  return call(async () => fromConnectionTestResult(await TestConnectionBinding(toProfile(connection))));
}
