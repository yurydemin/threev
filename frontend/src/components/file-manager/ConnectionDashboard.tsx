import { useEffect, useState } from 'react';
import { AlertTriangle, Database, Loader2, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn, formatBytes } from '../../lib/utils';
import { getBucketSize } from '../../lib/wails/fileManager';
import { toast } from '../../lib/toast';
import { ApiError } from '../../lib/wails/errors';
import { useFileManagerStore } from '../../stores/useFileManagerStore';
import { Button } from '../ui/Button';
import { Tooltip } from '../ui/Tooltip';
import type { BucketSizeResult } from '../../types';

/** Caps in-flight `GetBucketSize` calls so a profile with many buckets doesn't hammer S3/the UI thread all at once. */
const MAX_CONCURRENT_SIZE_FETCHES = 4;

type BucketSizeState =
  | { status: 'loading' }
  | { status: 'done'; result: BucketSizeResult }
  | { status: 'error' };

/** Matches `FileRow.tsx`'s `formatModified` convention exactly (same hardcoded locale — a pre-existing, out-of-scope inconsistency). */
function formatCreationDate(creationDate: string): string {
  if (!creationDate) return '';
  const date = new Date(creationDate);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleDateString('ru-RU', { year: 'numeric', month: 'short', day: 'numeric' });
}

/**
 * Runs `fetchOne` over `names` with at most `MAX_CONCURRENT_SIZE_FETCHES`
 * calls in flight — a worker-pool over a shared cursor rather than chunking
 * into batches of 4, so a fast bucket's slot is reused immediately instead
 * of the whole batch waiting on its slowest member. `isCancelled` is
 * rechecked before each new item is claimed so an unmounted dashboard stops
 * starting new fetches (in-flight ones still resolve, but `fetchOne` itself
 * guards the resulting state update).
 */
async function runBucketSizePool(
  names: string[],
  isCancelled: () => boolean,
  fetchOne: (name: string) => Promise<void>,
): Promise<void> {
  let cursor = 0;
  async function worker() {
    for (;;) {
      if (isCancelled() || cursor >= names.length) return;
      const name = names[cursor];
      cursor += 1;
      await fetchOne(name);
    }
  }
  const workerCount = Math.min(MAX_CONCURRENT_SIZE_FETCHES, names.length);
  await Promise.all(Array.from({ length: workerCount }, () => worker()));
}

/**
 * Center-panel dashboard shown after connecting to a profile, before a
 * bucket is picked (replaces the old dead "select a bucket" placeholder —
 * see task notes on the rejected right-click "Bucket properties" modal this
 * supersedes). Size/object-count per bucket is fetched automatically on
 * mount, bounded to `MAX_CONCURRENT_SIZE_FETCHES` concurrent requests, not
 * gated behind any button click.
 *
 * Reads `useFileManagerStore` directly rather than taking props — same
 * rationale as `BucketPanel`/`Toolbar`.
 *
 * Per-bucket size state is local component state, not the Zustand store: it
 * is transient display data that should always re-fetch fresh on mount
 * rather than persist/go stale across visits to this screen.
 */
export function ConnectionDashboard() {
  const { t } = useTranslation();
  const activeProfileId = useFileManagerStore((state) => state.activeProfileId);
  const buckets = useFileManagerStore((state) => state.buckets);
  const selectBucket = useFileManagerStore((state) => state.selectBucket);

  const [sizes, setSizes] = useState<Record<string, BucketSizeState>>({});

  // `isCancelled` defaults to "never cancelled" for the manual
  // recalculate/retry button handlers below, which fire while the
  // component is known to be mounted and don't need the effect's
  // per-run cancellation tracking.
  async function fetchOne(profileId: number, bucket: string, isCancelled: () => boolean = () => false) {
    setSizes((prev) => ({ ...prev, [bucket]: { status: 'loading' } }));
    try {
      const result = await getBucketSize(profileId, bucket, '');
      if (isCancelled()) return;
      setSizes((prev) => ({ ...prev, [bucket]: { status: 'done', result } }));
    } catch (err) {
      console.error('[ConnectionDashboard] getBucketSize failed:', err);
      if (isCancelled()) return;
      setSizes((prev) => ({ ...prev, [bucket]: { status: 'error' } }));
      toast.error(
        err instanceof ApiError ? err.message : t('fileManager.dashboard.recalculateError'),
        err instanceof ApiError ? err.raw : undefined,
      );
    }
  }

  useEffect(() => {
    // A local closure variable per effect run (matching PropertiesModal.tsx's
    // `let cancelled = false` pattern), not a component-level ref: a ref
    // shared across renders would get reset to `false` by THIS effect's own
    // setup the instant a new run starts, un-cancelling the previous run's
    // still-in-flight fetches and letting a stale profile's results land in
    // `sizes` after switching profiles/buckets mid-fetch.
    let cancelled = false;
    if (activeProfileId === null || buckets.length === 0) {
      setSizes({});
      return () => {
        cancelled = true;
      };
    }
    setSizes({});
    void runBucketSizePool(
      buckets.map((bucket) => bucket.name),
      () => cancelled,
      (name) => fetchOne(activeProfileId, name, () => cancelled),
    );
    return () => {
      cancelled = true;
    };
    // Deliberately keyed only on the profile/bucket-list identity — `t` and
    // `fetchOne` are recreated every render but shouldn't restart the pool.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeProfileId, buckets]);

  if (activeProfileId === null) return null;

  if (buckets.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-fg-muted">{t('fileManager.dashboard.empty')}</p>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto p-6">
      <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-4">
        {buckets.map((bucket) => {
          const state = sizes[bucket.name];
          return (
            <div
              key={bucket.name}
              role="button"
              tabIndex={0}
              onClick={() => selectBucket(bucket.name)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  selectBucket(bucket.name);
                }
              }}
              title={bucket.name}
              className={cn(
                'flex cursor-pointer flex-col gap-3 rounded border border-border bg-bg-secondary p-4 text-left',
                'transition-colors duration-fast hover:border-accent',
                'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
              )}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2">
                  <Database className="h-4 w-4 shrink-0 text-fg-secondary" aria-hidden="true" />
                  <span className="truncate text-sm font-semibold text-fg-primary">{bucket.name}</span>
                </div>
                <Tooltip content={t('fileManager.dashboard.recalculate')}>
                  <Button
                    iconOnly
                    variant="ghost"
                    className="h-7 w-7"
                    aria-label={t('fileManager.dashboard.recalculate')}
                    onClick={(event) => {
                      event.stopPropagation();
                      void fetchOne(activeProfileId, bucket.name);
                    }}
                  >
                    <RefreshCw
                      className={cn('h-3.5 w-3.5', state?.status === 'loading' && 'animate-spin')}
                      aria-hidden="true"
                    />
                  </Button>
                </Tooltip>
              </div>

              <p className="text-xs text-fg-muted">{formatCreationDate(bucket.creationDate)}</p>

              <div className="border-t border-border pt-3 text-[13px]">
                {!state || state.status === 'loading' ? (
                  <div className="flex items-center gap-2 text-fg-muted">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                    <span>{t('fileManager.dashboard.calculating')}</span>
                  </div>
                ) : state.status === 'error' ? (
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex items-center gap-1.5 text-xs text-danger">
                      <AlertTriangle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                      {t('fileManager.dashboard.recalculateError')}
                    </span>
                    <Tooltip content={t('fileManager.dashboard.retry')}>
                      <Button
                        iconOnly
                        variant="ghost"
                        className="h-7 w-7"
                        aria-label={t('fileManager.dashboard.retry')}
                        onClick={(event) => {
                          event.stopPropagation();
                          void fetchOne(activeProfileId, bucket.name);
                        }}
                      >
                        <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
                      </Button>
                    </Tooltip>
                  </div>
                ) : (
                  <div className="flex flex-col gap-1">
                    <p className="font-medium text-fg-primary">{formatBytes(state.result.totalBytes)}</p>
                    <p className="text-fg-secondary">
                      {t('fileManager.dashboard.objectsCount', { count: state.result.objectCount })}
                    </p>
                    {state.result.truncated && (
                      <div className="flex items-start gap-1.5 text-warning">
                        <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" aria-hidden="true" />
                        <p className="text-xs">{t('fileManager.dashboard.truncatedNotice')}</p>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
