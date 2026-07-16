import { Menu, MenuButton, MenuItem, MenuItems } from '@headlessui/react';
import { AlertTriangle, Copy, MoreHorizontal, Pencil, Trash2, Zap } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cn } from '../../lib/utils';
import { Button } from '../ui/Button';
import { Tooltip } from '../ui/Tooltip';
import type { ConnectionSummary } from '../../types';

export interface ConnectionCardProps {
  connection: ConnectionSummary;
  onConnect: (connection: ConnectionSummary) => void;
  onEdit: (connection: ConnectionSummary) => void;
  onDuplicate: (connection: ConnectionSummary) => void;
  onDelete: (connection: ConnectionSummary) => void;
  onTest: (connection: ConnectionSummary) => void;
}

const MENU_ITEM_CLASSES =
  'flex w-full items-center gap-2 px-3 py-1.5 text-left text-[13px] text-fg-primary data-[focus]:bg-bg-tertiary';

/**
 * Connection card per docs/03-ux-ui-spec.md section 5.2.
 *
 * "Подключиться" enters the File Manager for this profile (Stage 2, Block
 * F) — it loads the bucket list via `useFileManagerStore.enterProfile` and
 * switches the top-level screen, both driven by the `onConnect` callback
 * from `App.tsx`.
 *
 * The status dot is always rendered `--fg-muted` ("не проверялось"): there
 * is still no live/persisted connection-health state, so faking green/red
 * here would be dishonest UI.
 *
 * `connection.hasCredentials === false` (Block G — a profile imported via
 * "Импорт" whose blank credentials haven't been filled in yet) surfaces a
 * warning badge on its own row below the header (not inline with the name —
 * a `shrink-0` badge sharing that row with the "Подключиться" button
 * overflowed past its flex parent's shrunk width at the card's minimum
 * grid size, visually spilling onto the button) — every menu action still
 * works normally (they all go through `ConnectionService.GetProfile`, which
 * now tolerates a blank `SecretAccessKey`, see `resolve.go`'s
 * `ResolveProfile`), but "Подключиться" itself is disabled with an
 * explanatory `Tooltip` rather than left clickable: connecting always fails
 * for a credential-less profile, and offering a doomed-to-fail action isn't
 * useful even now that it fails with a clean error instead of a confusing
 * one.
 */
export function ConnectionCard({
  connection,
  onConnect,
  onEdit,
  onDuplicate,
  onDelete,
  onTest,
}: ConnectionCardProps) {
  const { t } = useTranslation();
  return (
    <div
      className={cn(
        'flex flex-col gap-2 rounded border border-border bg-bg-secondary p-4',
        'transition-colors duration-fast hover:border-accent',
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className="h-2 w-2 shrink-0 rounded-full bg-fg-muted"
            aria-hidden="true"
            title={t('connections.card.notChecked')}
          />
          <span className="min-w-0 truncate text-sm font-semibold text-fg-primary">{connection.name}</span>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {connection.hasCredentials ? (
            <Button variant="primary" onClick={() => onConnect(connection)}>
              {t('connections.card.connect')}
            </Button>
          ) : (
            // Disabled rather than left clickable-but-doomed: connecting
            // with blank credentials always fails, and previously did so
            // with a confusing low-level error (see resolve.go's
            // ResolveProfile fix). A disabled native <button> gets
            // `pointer-events: none` (Button.tsx), which stops it from ever
            // receiving the hover Tooltip listens for - wrapping it in a
            // plain <span> (still `pointer-events: auto`) gives the
            // tooltip something to attach to that the disabled button's own
            // pointer-events-none can't swallow.
            <Tooltip content={t('connections.card.requiresCredentialsTooltip')}>
              <span className="inline-flex">
                <Button variant="primary" disabled>
                  {t('connections.card.connect')}
                </Button>
              </span>
            </Tooltip>
          )}

          <Menu as="div" className="relative shrink-0">
            <Tooltip content={t('connections.card.actionsMenu')}>
              <MenuButton
                aria-label={t('connections.card.actionsMenu')}
                className={cn(
                  'flex h-8 w-8 items-center justify-center rounded-sm text-fg-secondary',
                  'transition-colors duration-fast hover:bg-bg-tertiary',
                  'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
                )}
              >
                <MoreHorizontal className="h-4 w-4" aria-hidden="true" />
              </MenuButton>
            </Tooltip>
            <MenuItems
              transition
              anchor={{ to: 'bottom end', gap: 4 }}
              className={cn(
                'z-50 w-44 rounded border border-border bg-bg-elevated py-1',
                'shadow-[0_4px_16px_rgba(0,0,0,0.20)] focus:outline-none',
                'transition duration-fast ease-out data-[closed]:scale-95 data-[closed]:opacity-0',
              )}
            >
              <MenuItem>
                <button type="button" className={MENU_ITEM_CLASSES} onClick={() => onEdit(connection)}>
                  <Pencil className="h-3.5 w-3.5" aria-hidden="true" />
                  {t('connections.card.edit')}
                </button>
              </MenuItem>
              <MenuItem>
                <button type="button" className={MENU_ITEM_CLASSES} onClick={() => onDuplicate(connection)}>
                  <Copy className="h-3.5 w-3.5" aria-hidden="true" />
                  {t('connections.card.duplicate')}
                </button>
              </MenuItem>
              <MenuItem>
                <button type="button" className={MENU_ITEM_CLASSES} onClick={() => onTest(connection)}>
                  <Zap className="h-3.5 w-3.5" aria-hidden="true" />
                  {t('connections.card.test')}
                </button>
              </MenuItem>
              <div className="my-1 border-t border-border" />
              <MenuItem>
                <button
                  type="button"
                  className={cn(MENU_ITEM_CLASSES, 'text-danger')}
                  onClick={() => onDelete(connection)}
                >
                  <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
                  {t('common.delete')}
                </button>
              </MenuItem>
            </MenuItems>
          </Menu>
        </div>
      </div>

      {!connection.hasCredentials && (
        <Tooltip content={t('connections.card.requiresCredentialsTooltip')}>
          <span
            tabIndex={0}
            className="flex w-fit items-center gap-1 rounded-sm border border-warning px-1.5 py-0.5 text-[11px] font-medium text-warning"
          >
            <AlertTriangle className="h-3 w-3 shrink-0" aria-hidden="true" />
            {t('connections.card.requiresCredentials')}
          </span>
        </Tooltip>
      )}

      <p className="truncate font-mono text-xs text-fg-secondary">
        {connection.endpointUrl}
        {connection.region ? ` • ${connection.region}` : ''}
      </p>
    </div>
  );
}
