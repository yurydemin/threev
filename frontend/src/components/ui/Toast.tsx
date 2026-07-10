import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react';
import { cn } from '../../lib/utils';
import type { ToastType } from '../../stores/useToastStore';

export interface ToastProps {
  id: number;
  type: ToastType;
  message: string;
  onDismiss: (id: number) => void;
}

const TYPE_CONFIG: Record<ToastType, { icon: typeof CheckCircle2; border: string; iconColor: string }> = {
  success: { icon: CheckCircle2, border: 'border-l-success', iconColor: 'text-success' },
  error: { icon: XCircle, border: 'border-l-danger', iconColor: 'text-danger' },
  warning: { icon: AlertTriangle, border: 'border-l-warning', iconColor: 'text-warning' },
  info: { icon: Info, border: 'border-l-accent', iconColor: 'text-accent' },
};

/**
 * Single toast/notification, per docs/03-ux-ui-spec.md section 4.8.
 *
 * Rendered exclusively by `ToastContainer` — never mounted directly, same
 * convention as `ContextMenu` items. The close (`X`) button is always shown
 * regardless of `type`: the spec only explicitly calls it out for `error`,
 * but nothing suggests `success`/`info`/`warning` shouldn't be dismissible
 * too, and `ToastContainer`'s `dismiss` already supports any id.
 */
export function Toast({ id, type, message, onDismiss }: ToastProps) {
  const { icon: Icon, border, iconColor } = TYPE_CONFIG[type];

  return (
    <div
      role="status"
      className={cn(
        'flex w-[360px] max-w-[360px] items-start gap-2.5 rounded border border-l-[3px] border-border bg-bg-elevated py-2.5 pl-3.5 pr-2.5',
        'shadow-[0_4px_12px_rgba(0,0,0,0.15)]',
        'animate-toast-in',
        border,
      )}
    >
      <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', iconColor)} aria-hidden="true" />
      <p className="min-w-0 flex-1 break-words text-[13px] text-fg-primary">{message}</p>
      <button
        type="button"
        onClick={() => onDismiss(id)}
        aria-label="Закрыть уведомление"
        className={cn(
          'flex h-5 w-5 shrink-0 items-center justify-center rounded-sm text-fg-muted',
          'transition-colors duration-fast hover:bg-bg-tertiary hover:text-fg-primary',
        )}
      >
        <X className="h-3.5 w-3.5" aria-hidden="true" />
      </button>
    </div>
  );
}
