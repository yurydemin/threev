import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, ChevronDown, ChevronRight, Folder, Loader2 } from 'lucide-react';
import { cn } from '../../lib/utils';
import { listObjects } from '../../lib/wails/fileManager';
import { ApiError } from '../../lib/wails/errors';
import type { ObjectEntry } from '../../types';
import { Button } from '../ui/Button';

const ROOT_PREFIX = '';
/** Matches `useFileManagerStore`'s own defaults, for the same folders-first, alphabetical ordering the main list shows. */
const SORT_BY = 'name';
const SORT_ORDER = 'asc';
const INDENT_PX = 16;

/**
 * Per-node fetch state. Deliberately separate from the "is this node
 * expanded" UI concern (tracked in `expanded` below): folding this into a
 * single `collapsed|loading|expanded|error` status would make "collapsed,
 * never fetched" indistinguishable from "collapsed, already fetched and
 * cached empty" — the latter must NOT re-fetch on re-expand, the former
 * must.
 */
interface NodeState {
  status: 'loading' | 'loaded' | 'error';
  /** Only folders (`isFolder: true`) — `listObjects`'s `CommonPrefixes` entries. */
  children: ObjectEntry[];
  /** Set when the last fetch page was truncated; consumed by "Показать ещё". */
  nextToken?: string;
  error?: string;
}

export interface FolderTreeProps {
  profileId: number;
  bucket: string;
  /** The sibling `<Input>`'s current value — used to highlight a matching tree node, nothing more (simple string equality, no path normalization). */
  selectedPrefix: string;
  onSelect: (prefix: string) => void;
}

function folderLabel(key: string): string {
  const trimmed = key.replace(/\/$/, '');
  const lastSlash = trimmed.lastIndexOf('/');
  return lastSlash === -1 ? trimmed : trimmed.slice(lastSlash + 1);
}

/**
 * Local folder browser for `DestinationPickerModal`'s destination
 * bucket/prefix picker. Deliberately local `useState`, not
 * `useFileManagerStore`: that store's `entries`/pagination belong to the
 * main `FileList` behind this modal and must not be mutated by picker
 * navigation. Each node lazy-fetches its children on first expand
 * (`listObjects` with the existing `Delimiter="/"` folder listing) and
 * caches them, so collapsing/re-expanding a branch never re-fetches.
 */
export function FolderTree({ profileId, bucket, selectedPrefix, onSelect }: FolderTreeProps) {
  const { t } = useTranslation();
  const [nodes, setNodes] = useState<Map<string, NodeState>>(new Map());
  const [expanded, setExpanded] = useState<Set<string>>(new Set([ROOT_PREFIX]));

  const loadNode = useCallback(
    async (prefix: string, continuationToken?: string) => {
      setNodes((prev) => {
        const next = new Map(prev);
        const existing = next.get(prefix);
        next.set(prefix, { status: 'loading', children: existing?.children ?? [], nextToken: existing?.nextToken });
        return next;
      });
      try {
        const response = await listObjects({
          profileId,
          bucket,
          prefix,
          continuationToken: continuationToken ?? '',
          sortBy: SORT_BY,
          sortOrder: SORT_ORDER,
          refresh: false,
        });
        const folders = response.entries.filter((entry) => entry.isFolder);
        setNodes((prev) => {
          const next = new Map(prev);
          const existing = next.get(prefix);
          const children = continuationToken ? [...(existing?.children ?? []), ...folders] : folders;
          next.set(prefix, {
            status: 'loaded',
            children,
            nextToken: response.isTruncated ? response.nextContinuationToken : undefined,
          });
          return next;
        });
      } catch (err) {
        const message = err instanceof ApiError ? err.message : t('fileManager.destinationPickerModal.treeLoadError');
        setNodes((prev) => {
          const next = new Map(prev);
          next.set(prefix, { status: 'error', children: prev.get(prefix)?.children ?? [], error: message });
          return next;
        });
      }
    },
    [profileId, bucket, t],
  );

  // Root is pre-expanded/fetched on mount, and the whole tree resets
  // whenever the destination bucket changes (`loadNode`'s identity changes
  // with it) — otherwise switching buckets in the sibling `Select` would
  // leave stale folders from the previous bucket visible.
  useEffect(() => {
    setNodes(new Map());
    setExpanded(new Set([ROOT_PREFIX]));
    void loadNode(ROOT_PREFIX);
  }, [loadNode]);

  function toggleExpand(prefix: string) {
    const willExpand = !expanded.has(prefix);
    setExpanded((prev) => {
      const next = new Set(prev);
      if (willExpand) {
        next.add(prefix);
      } else {
        next.delete(prefix);
      }
      return next;
    });
    if (willExpand) {
      const state = nodes.get(prefix);
      if (!state || state.status === 'error') {
        void loadNode(prefix);
      }
    }
  }

  function renderNode(prefix: string, label: string, depth: number): ReactNode {
    const state = nodes.get(prefix);
    const isExpanded = expanded.has(prefix);
    const isRoot = prefix === ROOT_PREFIX;
    const isSelected = selectedPrefix === prefix;
    const indent = depth * INDENT_PX + 4;

    return (
      <div key={prefix || '__root__'}>
        <div
          className={cn(
            'flex items-center gap-1 rounded-sm py-1 pr-2 text-[13px] transition-colors duration-fast hover:bg-bg-tertiary',
            isSelected && 'bg-accent-subtle text-accent',
          )}
          style={{ paddingLeft: indent }}
        >
          {isRoot ? (
            <span className="h-4 w-4 shrink-0" />
          ) : (
            <button
              type="button"
              onClick={() => toggleExpand(prefix)}
              className="flex h-4 w-4 shrink-0 items-center justify-center text-fg-muted hover:text-fg-primary"
              aria-label={isExpanded ? t('fileManager.destinationPickerModal.treeCollapse') : t('fileManager.destinationPickerModal.treeExpand')}
            >
              {isExpanded ? (
                <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5" aria-hidden="true" />
              )}
            </button>
          )}
          <button
            type="button"
            onClick={() => onSelect(prefix)}
            className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-left"
          >
            <Folder className="h-3.5 w-3.5 shrink-0 text-fg-muted" aria-hidden="true" />
            <span className="truncate" title={label}>
              {label}
            </span>
          </button>
          {state?.status === 'loading' && (
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-fg-muted" aria-hidden="true" />
          )}
        </div>
        {isExpanded && (
          <div>
            {state?.status === 'error' ? (
              <div className="flex items-center gap-2 py-1 pr-2 text-2xs text-danger" style={{ paddingLeft: indent + INDENT_PX }}>
                <AlertTriangle className="h-3 w-3 shrink-0" aria-hidden="true" />
                <span className="truncate">{state.error}</span>
                <button
                  type="button"
                  className="shrink-0 underline hover:no-underline"
                  onClick={() => void loadNode(prefix)}
                >
                  {t('common.retry')}
                </button>
              </div>
            ) : state?.status === 'loading' && state.children.length === 0 ? (
              <div className="flex items-center gap-2 py-1 text-2xs text-fg-muted" style={{ paddingLeft: indent + INDENT_PX }}>
                <Loader2 className="h-3 w-3 shrink-0 animate-spin" aria-hidden="true" />
                <span>{t('common.loading')}</span>
              </div>
            ) : state?.status === 'loaded' && state.children.length === 0 ? (
              <p className="py-1 text-2xs text-fg-muted" style={{ paddingLeft: indent + INDENT_PX }}>
                {t('fileManager.destinationPickerModal.treeEmpty')}
              </p>
            ) : (
              <>
                {(state?.children ?? []).map((child) => renderNode(child.key, folderLabel(child.key), depth + 1))}
                {state?.nextToken && (
                  <div style={{ paddingLeft: indent + INDENT_PX }} className="py-1">
                    {/* Disabled while a page fetch for this node is already
                        in flight - `state.nextToken` is preserved (not
                        cleared) during that fetch (see loadNode's optimistic
                        update), so an un-disabled button here would let a
                        double-click fire two requests for the same
                        continuationToken and append duplicate folders. */}
                    <Button
                      variant="ghost"
                      disabled={state.status === 'loading'}
                      onClick={() => void loadNode(prefix, state.nextToken)}
                    >
                      {t('fileManager.destinationPickerModal.treeShowMore')}
                    </Button>
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="max-h-[280px] overflow-y-auto rounded border border-border bg-bg-secondary p-1">
      {renderNode(ROOT_PREFIX, t('fileManager.destinationPickerModal.treeRoot'), 0)}
    </div>
  );
}
