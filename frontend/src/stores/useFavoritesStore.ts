import { create } from 'zustand';
import { addFavorite as addFavoriteApi, getFavorites, removeFavorite as removeFavoriteApi } from '../lib/wails/favorites';
import { ApiError } from '../lib/wails/errors';
import type { Favorite } from '../types';

interface FavoritesState {
  favorites: Favorite[];
  isLoading: boolean;
  error: string | null;

  fetchFavorites: () => Promise<void>;
  /** Adds a favorite then re-fetches, same "backend is the source of truth" pattern as `useTransferStore`'s mutating actions. */
  addFavorite: (profileId: number, bucket: string, prefix: string) => Promise<void>;
  /** Removes a favorite then re-fetches. Same rationale as `addFavorite`. */
  removeFavorite: (id: number) => Promise<void>;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}

/**
 * Global favorites list (bookmarked bucket/prefix locations across every
 * profile), backed by `FavoritesService` via `lib/wails/favorites.ts`.
 *
 * Unlike `useFileManagerStore`, this store is not scoped to any one
 * profile/session — `GetFavorites()` itself is global (see
 * `lib/wails/favorites.ts`'s doc comment) — so `Sidebar` reads it directly
 * regardless of which profile (if any) is currently connected.
 *
 * `fetchFavorites` is called once from `App.tsx`'s mount effect (alongside
 * `useConnectionStore`'s `fetchConnections`), not lazily self-initialized on
 * first import — same convention as every other top-level store in this app.
 */
export const useFavoritesStore = create<FavoritesState>()((set, get) => ({
  favorites: [],
  isLoading: false,
  error: null,

  fetchFavorites: async () => {
    set({ isLoading: true, error: null });
    try {
      const favorites = await getFavorites();
      set({ favorites, isLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), isLoading: false });
    }
  },

  addFavorite: async (profileId, bucket, prefix) => {
    try {
      await addFavoriteApi(profileId, bucket, prefix);
      await get().fetchFavorites();
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  removeFavorite: async (id) => {
    try {
      await removeFavoriteApi(id);
      await get().fetchFavorites();
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },
}));

/**
 * Derived-boolean helper for the `Toolbar` star toggle — plain function
 * (not a store action) taking the store's own current `favorites`, per this
 * codebase's convention for cheap derived lookups computed from already-
 * loaded state (cf. `useFileManagerStore`'s inline `canGoBack`/`canGoForward`
 * derivations, computed by consumers rather than stored).
 */
export function isFavorite(profileId: number | null, bucket: string | null, prefix: string): boolean {
  if (profileId === null || bucket === null) return false;
  return useFavoritesStore
    .getState()
    .favorites.some((favorite) => favorite.profileId === profileId && favorite.bucket === bucket && favorite.prefix === prefix);
}
