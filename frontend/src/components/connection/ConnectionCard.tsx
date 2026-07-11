import { Menu, MenuButton, MenuItem, MenuItems } from '@headlessui/react';
import { Copy, MoreHorizontal, Pencil, Trash2, Zap } from 'lucide-react';
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
          <span className="truncate text-sm font-semibold text-fg-primary">{connection.name}</span>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          <Button variant="primary" onClick={() => onConnect(connection)}>
            {t('connections.card.connect')}
          </Button>

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

      <p className="truncate font-mono text-xs text-fg-secondary">
        {connection.endpointUrl}
        {connection.region ? ` • ${connection.region}` : ''}
      </p>
    </div>
  );
}
