/**
 * Typed wrapper around the generated `wailsjs/go/favorites/FavoritesService`
 * bindings.
 *
 * Same responsibilities/rationale as `./connection.ts`: converts between the
 * frontend-domain `Favorite` (`types/index.ts`, camelCase) and the
 * wailsjs-generated `domain.Favorite` DTO class (PascalCase), and normalizes
 * rejected promises into `ApiError` via `call`.
 *
 * Do not import `wailsjs/go/**` anywhere else in the app — go through this
 * module instead.
 */
import { AddFavorite, GetFavorites, RemoveFavorite } from '../../../wailsjs/go/favorites/FavoritesService';
import { domain } from '../../../wailsjs/go/models';
import type { Favorite } from '../../types';
import { call, toIsoString } from './errors';

function fromFavoriteDTO(dto: domain.Favorite): Favorite {
  return {
    id: dto.ID,
    profileId: dto.ProfileID,
    profileName: dto.ProfileName,
    bucket: dto.Bucket,
    prefix: dto.Prefix,
    createdAt: toIsoString(dto.CreatedAt),
  };
}

export async function addFavorite(profileId: number, bucket: string, prefix: string): Promise<Favorite> {
  return call(async () => fromFavoriteDTO(await AddFavorite(profileId, bucket, prefix)));
}

export async function removeFavorite(id: number): Promise<void> {
  return call(() => RemoveFavorite(id));
}

/** Fetches every favorite across every profile (backend is not filtered by the caller's currently active profile). */
export async function getFavorites(): Promise<Favorite[]> {
  return call(async () => (await GetFavorites()).map(fromFavoriteDTO));
}
