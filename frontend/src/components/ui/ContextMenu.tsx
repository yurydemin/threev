import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../../lib/utils';

export interface ContextMenuItem {
  /** Renders a `1px solid border` separator instead of a clickable row. */
  separator?: boolean;
  label?: string;
  icon?: ReactNode;
  onClick?: () => void;
  destructive?: boolean;
  disabled?: boolean;
}

export interface ContextMenuProps {
  /** Viewport coordinates (e.g. `event.clientX/clientY` from a `contextmenu` handler). */
  x: number;
  y: number;
  items: ContextMenuItem[];
  onClose: () => void;
}

/**
 * Context menu per docs/03-ux-ui-spec.md section 4.11.
 *
 * Unlike the Headless UI `Menu` used in `ConnectionCard` (anchored to a
 * trigger button), this is positioned at an arbitrary point — the cursor
 * location on right-click — via `position: fixed` + a portal into
 * `document.body`. Closes on outside click and `Escape`.
 */
export function ContextMenu({ x, y, items, onClose }: ContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ x, y });

  useEffect(() => {
    function handlePointerDown(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        onClose();
      }
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [onClose]);

  // Clamp so the menu never renders past the right/bottom viewport edge.
  useLayoutEffect(() => {
    const el = menuRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const clampedX = Math.min(x, window.innerWidth - rect.width - 4);
    const clampedY = Math.min(y, window.innerHeight - rect.height - 4);
    setPosition({ x: Math.max(4, clampedX), y: Math.max(4, clampedY) });
  }, [x, y]);

  return createPortal(
    <div
      ref={menuRef}
      role="menu"
      style={{ left: position.x, top: position.y }}
      className={cn(
        'fixed z-50 w-56 rounded border border-border bg-bg-elevated py-1',
        'shadow-[0_4px_16px_rgba(0,0,0,0.20)]',
      )}
    >
      {items.map((item, index) =>
        item.separator ? (
          // eslint-disable-next-line react/no-array-index-key
          <div key={index} className="my-1 border-t border-border" role="separator" />
        ) : (
          <button
            // eslint-disable-next-line react/no-array-index-key
            key={index}
            type="button"
            role="menuitem"
            disabled={item.disabled}
            onClick={() => {
              onClose();
              item.onClick?.();
            }}
            className={cn(
              'flex h-8 w-full items-center gap-2 px-3 text-left text-[13px]',
              'transition-colors duration-fast',
              item.disabled
                ? 'cursor-not-allowed text-fg-muted opacity-50'
                : item.destructive
                  ? 'text-danger hover:bg-bg-tertiary'
                  : 'text-fg-primary hover:bg-bg-tertiary',
            )}
          >
            {item.icon}
            {item.label}
          </button>
        ),
      )}
    </div>,
    document.body,
  );
}
