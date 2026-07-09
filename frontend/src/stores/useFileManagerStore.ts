import { create } from 'zustand';
import { listBuckets, listObjects } from '../lib/wails/fileManager';
import { ApiError } from '../lib/wails/errors';
import type { Bucket, ObjectEntry } from '../types';

/** One entry of the browser-style back/forward navigation stack. */
interface HistoryEntry {
  bucket: string;
  prefix: string;
}

interface FileManagerState {
  activeProfileId: number | null;
  activeProfileName: string | null;
  buckets: Bucket[];
  isLoadingBuckets: boolean;
  bucketsError: string | null;

  selectedBucket: string | null;
  currentPrefix: string;
  entries: ObjectEntry[];
  nextContinuationToken: string;
  isTruncated: boolean;
  sortBy: string;
  sortOrder: string;
  searchQuery: string;
  isLoadingEntries: boolean;
  entriesError: string | null;

  /**
   * Browser-style history stack of visited {bucket, prefix} locations.
   * `historyIndex` points at the current location within `history`.
   * `canGoBack`/`canGoForward` are intentionally NOT stored here — they are
   * trivial derivations (`historyIndex > 0` / `historyIndex < history.length
   * - 1`) that consumers (e.g. the Toolbar in Block G) can compute directly
   * from `history`/`historyIndex` without another piece of state to keep in
   * sync.
   */
  history: HistoryEntry[];
  historyIndex: number;

  /** Loads the bucket list for a profile and resets all bucket/entry state. */
  enterProfile: (profileId: number, profileName: string) => Promise<void>;
  /** Navigates to the root of `bucket`, pushing a new history entry. */
  selectBucket: (bucket: string) => Promise<void>;
  /** Navigates to `prefix` within the currently selected bucket, pushing a new history entry. */
  navigateToPrefix: (prefix: string) => Promise<void>;
  goBack: () => Promise<void>;
  goForward: () => Promise<void>;
  /** Fetches the next page via `nextContinuationToken`, appending to `entries`. */
  loadMore: () => Promise<void>;
  /** Changes sort and reloads (server-side re-sort of the cached page, no new S3 round-trip). */
  setSort: (sortBy: string, sortOrder: string) => Promise<void>;
  /** Purely local filter over already-loaded `entries` — no backend call. */
  setSearchQuery: (query: string) => void;
  /** Reloads the current bucket/prefix with `Refresh: true` (bypasses backend cache). */
  refresh: () => Promise<void>;
  /** Clears all File Manager state (called when leaving back to Connections). */
  reset: () => void;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}

const DEFAULT_SORT_BY = 'name';
const DEFAULT_SORT_ORDER = 'asc';

/** Entry/bucket-browsing state reset both on `enterProfile` and `reset`. */
const initialBrowsingState = {
  selectedBucket: null as string | null,
  currentPrefix: '',
  entries: [] as ObjectEntry[],
  nextContinuationToken: '',
  isTruncated: false,
  sortBy: DEFAULT_SORT_BY,
  sortOrder: DEFAULT_SORT_ORDER,
  searchQuery: '',
  isLoadingEntries: false,
  entriesError: null as string | null,
  history: [] as HistoryEntry[],
  historyIndex: -1,
};

/**
 * File Manager navigation/state store, backed by `FileManagerService` via
 * `lib/wails/fileManager.ts`.
 *
 * Errors are captured into `bucketsError`/`entriesError` rather than
 * re-thrown, following the same pattern as `useConnectionStore`.
 */
export const useFileManagerStore = create<FileManagerState>()((set, get) => {
  /** Shared fetch/append logic for `selectBucket`/`navigateToPrefix`/`goBack`/`goForward`/`loadMore`/`setSort`/`refresh`. */
  async function loadEntries(
    bucket: string,
    prefix: string,
    options: { refresh?: boolean; append?: boolean } = {},
  ) {
    const { refresh = false, append = false } = options;
    const state = get();
    set({ isLoadingEntries: true, entriesError: null });
    try {
      const response = await listObjects({
        profileId: state.activeProfileId ?? 0,
        bucket,
        prefix,
        continuationToken: append ? state.nextContinuationToken : '',
        sortBy: state.sortBy,
        sortOrder: state.sortOrder,
        refresh,
      });
      set({
        entries: append ? [...get().entries, ...response.entries] : response.entries,
        nextContinuationToken: response.nextContinuationToken,
        isTruncated: response.isTruncated,
        isLoadingEntries: false,
      });
    } catch (err) {
      set({ entriesError: errorMessage(err), isLoadingEntries: false });
    }
  }

  /**
   * Pushes a new location onto the history stack, discarding any "future"
   * entries beyond the current index (standard browser-history semantics:
   * navigating away after `goBack` overwrites what was ahead).
   */
  function pushHistory(entry: HistoryEntry) {
    const { history, historyIndex } = get();
    const truncated = history.slice(0, historyIndex + 1);
    truncated.push(entry);
    set({ history: truncated, historyIndex: truncated.length - 1 });
  }

  /** Applies a history entry (from `goBack`/`goForward`) as the current location and reloads. */
  async function gotoHistoryIndex(newIndex: number) {
    const target = get().history[newIndex];
    set({
      historyIndex: newIndex,
      selectedBucket: target.bucket,
      currentPrefix: target.prefix,
      entries: [],
      nextContinuationToken: '',
      isTruncated: false,
      searchQuery: '',
      entriesError: null,
    });
    await loadEntries(target.bucket, target.prefix);
  }

  return {
    activeProfileId: null,
    activeProfileName: null,
    buckets: [],
    isLoadingBuckets: false,
    bucketsError: null,

    ...initialBrowsingState,

    enterProfile: async (profileId, profileName) => {
      set({
        activeProfileId: profileId,
        activeProfileName: profileName,
        buckets: [],
        isLoadingBuckets: true,
        bucketsError: null,
        ...initialBrowsingState,
      });
      try {
        const buckets = await listBuckets(profileId);
        set({ buckets, isLoadingBuckets: false });
      } catch (err) {
        set({ bucketsError: errorMessage(err), isLoadingBuckets: false });
      }
    },

    selectBucket: async (bucket) => {
      set({
        selectedBucket: bucket,
        currentPrefix: '',
        entries: [],
        nextContinuationToken: '',
        isTruncated: false,
        searchQuery: '',
        entriesError: null,
      });
      pushHistory({ bucket, prefix: '' });
      await loadEntries(bucket, '');
    },

    navigateToPrefix: async (prefix) => {
      const bucket = get().selectedBucket;
      if (!bucket) return;
      set({
        currentPrefix: prefix,
        entries: [],
        nextContinuationToken: '',
        isTruncated: false,
        searchQuery: '',
        entriesError: null,
      });
      pushHistory({ bucket, prefix });
      await loadEntries(bucket, prefix);
    },

    goBack: async () => {
      const { historyIndex } = get();
      if (historyIndex <= 0) return;
      await gotoHistoryIndex(historyIndex - 1);
    },

    goForward: async () => {
      const { history, historyIndex } = get();
      if (historyIndex >= history.length - 1) return;
      await gotoHistoryIndex(historyIndex + 1);
    },

    loadMore: async () => {
      const { selectedBucket, currentPrefix, isTruncated, isLoadingEntries } = get();
      if (!selectedBucket || !isTruncated || isLoadingEntries) return;
      await loadEntries(selectedBucket, currentPrefix, { append: true });
    },

    setSort: async (sortBy, sortOrder) => {
      const { selectedBucket, currentPrefix } = get();
      set({ sortBy, sortOrder });
      if (!selectedBucket) return;
      await loadEntries(selectedBucket, currentPrefix);
    },

    setSearchQuery: (query) => set({ searchQuery: query }),

    refresh: async () => {
      const { selectedBucket, currentPrefix } = get();
      if (!selectedBucket) return;
      await loadEntries(selectedBucket, currentPrefix, { refresh: true });
    },

    reset: () =>
      set({
        activeProfileId: null,
        activeProfileName: null,
        buckets: [],
        isLoadingBuckets: false,
        bucketsError: null,
        ...initialBrowsingState,
      }),
  };
});
