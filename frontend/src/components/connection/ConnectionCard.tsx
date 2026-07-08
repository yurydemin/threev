import { Menu, MenuButton, MenuItem, MenuItems } from '@headlessui/react';
import { Copy, MoreHorizontal, Pencil, Trash2, Zap } from 'lucide-react';
import { cn } from '../../lib/utils';
import type { ConnectionSummary } from '../../types';

export interface ConnectionCardProps {
  connection: ConnectionSummary;
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
 * The spec's "Подключиться" button assumes a working file-manager screen,
 * which is out of scope for Stage 1 (bucket browsing lands in Stage 2). We
 * deliberately omit that button rather than render a permanently-disabled
 * one — a disabled button that will never become enabled in this build is
 * more misleading than simply not promising the feature yet.
 *
 * The status dot is always rendered `--fg-muted` ("не проверялось"): Stage 1
 * has no live/persisted connection-health state, so faking green/red here
 * would be dishonest UI.
 */
export function ConnectionCard({ connection, onEdit, onDuplicate, onDelete, onTest }: ConnectionCardProps) {
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
            title="Не проверялось"
          />
          <span className="truncate text-sm font-semibold text-fg-primary">{connection.name}</span>
        </div>

        <Menu as="div" className="relative shrink-0">
          <MenuButton
            aria-label="Действия с подключением"
            className={cn(
              'flex h-8 w-8 items-center justify-center rounded-sm text-fg-secondary',
              'transition-colors duration-fast hover:bg-bg-tertiary',
              'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
            )}
          >
            <MoreHorizontal className="h-4 w-4" aria-hidden="true" />
          </MenuButton>
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
                Редактировать
              </button>
            </MenuItem>
            <MenuItem>
              <button type="button" className={MENU_ITEM_CLASSES} onClick={() => onDuplicate(connection)}>
                <Copy className="h-3.5 w-3.5" aria-hidden="true" />
                Дублировать
              </button>
            </MenuItem>
            <MenuItem>
              <button type="button" className={MENU_ITEM_CLASSES} onClick={() => onTest(connection)}>
                <Zap className="h-3.5 w-3.5" aria-hidden="true" />
                Тестировать
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
                Удалить
              </button>
            </MenuItem>
          </MenuItems>
        </Menu>
      </div>

      <p className="truncate font-mono text-xs text-fg-secondary">
        {connection.endpointUrl}
        {connection.region ? ` • ${connection.region}` : ''}
      </p>
    </div>
  );
}
