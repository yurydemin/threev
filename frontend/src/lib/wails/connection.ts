/**
 * Typed wrapper around the generated `wailsjs/go/connection/ConnectionService`
 * bindings.
 *
 * Responsibilities:
 * - Convert between the frontend-domain types (`types/index.ts`, camelCase)
 *   and the wailsjs-generated Go DTO classes (`domain.*`, PascalCase).
 * - Normalize rejected promises (Wails surfaces Go errors as string
 *   rejections) into a single `ApiError` shape the rest of the app can rely
 *   on (see `./errors`).
 *
 * Do not import `wailsjs/go/**` anywhere else in the app — go through this
 * module instead.
 */
import {
  DeleteProfile,
  ExportProfiles,
  GetProfile,
  GetProfiles,
  ImportProfiles,
  SaveProfile,
  TestConnection as TestConnectionBinding,
} from '../../../wailsjs/go/connection/ConnectionService';
import { connection, domain } from '../../../wailsjs/go/models';
import type {
  Connection,
  ConnectionFormValues,
  ConnectionSummary,
  ConnectionTestResult,
  ImportProfilesResult,
} from '../../types';
import { call, toIsoString } from './errors';

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
    hasCredentials: dto.HasCredentials,
    hasProxy: dto.HasProxy,
  };
}

function fromImportResult(result: connection.ImportResult): ImportProfilesResult {
  return {
    importedCount: result.ImportedCount,
    skippedNames: result.SkippedNames ?? [],
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
    proxyUrl: profile.ProxyURL,
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
    ProxyURL: connection.proxyUrl,
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

/**
 * Shows a native "save file" dialog and writes every profile's non-secret
 * fields to the chosen path as JSON. Resolves normally (nothing written) if
 * the user cancels the dialog.
 */
export async function exportProfiles(): Promise<void> {
  return call(() => ExportProfiles());
}

/**
 * Shows a native "open file" dialog and creates a new profile (blank
 * credentials) for every entry in the chosen JSON file whose name doesn't
 * already exist. Resolves with an empty result if the user cancels the
 * dialog.
 */
export async function importProfiles(): Promise<ImportProfilesResult> {
  return call(async () => fromImportResult(await ImportProfiles()));
}
