import { create } from 'zustand';
import {
  deleteConnection as apiDeleteConnection,
  listConnections,
  saveConnection as apiSaveConnection,
} from '../lib/wails/connection';
import { ApiError } from '../lib/wails/errors';
import type { Connection, ConnectionFormValues, ConnectionSummary } from '../types';

interface ConnectionState {
  connections: ConnectionSummary[];
  selectedId: number | null;
  isLoading: boolean;
  error: string | null;

  fetchConnections: () => Promise<void>;
  saveConnection: (
    values: (Partial<Connection> & ConnectionFormValues) | Connection,
  ) => Promise<Connection | null>;
  deleteConnection: (id: number) => Promise<void>;
  selectConnection: (id: number | null) => void;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}

/**
 * Connection list state, backed by `ConnectionService` via `lib/wails/connection.ts`.
 *
 * Errors are captured into `error` rather than re-thrown, so screens/forms
 * can render them without needing try/catch of their own. `testConnection`
 * is intentionally NOT part of this store — it is transient UI feedback
 * owned locally by the connection form (see Stage 1, step 26).
 */
export const useConnectionStore = create<ConnectionState>()((set, get) => ({
  connections: [],
  selectedId: null,
  isLoading: false,
  error: null,

  fetchConnections: async () => {
    set({ isLoading: true, error: null });
    try {
      const connections = await listConnections();
      set({ connections, isLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), isLoading: false });
    }
  },

  saveConnection: async (values) => {
    set({ isLoading: true, error: null });
    try {
      const saved = await apiSaveConnection(values);
      await get().fetchConnections();
      set({ selectedId: saved.id, isLoading: false });
      return saved;
    } catch (err) {
      set({ error: errorMessage(err), isLoading: false });
      return null;
    }
  },

  deleteConnection: async (id) => {
    set({ isLoading: true, error: null });
    try {
      await apiDeleteConnection(id);
      const selectedId = get().selectedId === id ? null : get().selectedId;
      set({ selectedId });
      await get().fetchConnections();
      set({ isLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), isLoading: false });
    }
  },

  selectConnection: (id) => set({ selectedId: id }),
}));
