import { useState } from 'react';
import { AlertTriangle, Check, CheckCircle2, Copy, Info, X, XCircle } from 'lucide-react';
import { cn, copyToClipboard } from '../../lib/utils';
import { Tooltip } from './Tooltip';
import type { ToastType } from '../../stores/useToastStore';

export interface ToastProps {
  id: number;
  type: ToastType;
  message: string;
  /** Technical details behind `message` (UX-007) — renders a "Скопировать детали" button when set. */
  details?: string;
  onDismiss: (id: number) => void;
}

/** How long the copy button shows a checkmark before reverting to the copy icon. */
const COPY_CONFIRMATION_MS = 1500;

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
 *
 * `details` (only ever set for `type === 'error'`, see `lib/toast.ts`)
 * renders a small "Скопировать детали" icon button next to the close
 * button (UX-007) — clicking it copies the technical `ApiError.raw` text
 * via the shared `copyToClipboard` helper. No extra toast fires on success
 * (that would be noisy on top of the toast the user is already reading);
 * instead the icon itself swaps to a checkmark for `COPY_CONFIRMATION_MS`,
 * the standard "copy button" confirmation pattern.
 */
export function Toast({ id, type, message, details, onDismiss }: ToastProps) {
  const { icon: Icon, border, iconColor } = TYPE_CONFIG[type];
  const [isCopied, setIsCopied] = useState(false);

  async function handleCopyDetails() {
    if (!details) return;
    await copyToClipboard(details);
    setIsCopied(true);
    setTimeout(() => setIsCopied(false), COPY_CONFIRMATION_MS);
  }

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
      {type === 'error' && details && (
        <Tooltip content="Скопировать технические детали">
          <button
            type="button"
            onClick={() => void handleCopyDetails()}
            aria-label="Скопировать технические детали"
            className={cn(
              'flex h-5 w-5 shrink-0 items-center justify-center rounded-sm text-fg-muted',
              'transition-colors duration-fast hover:bg-bg-tertiary hover:text-fg-primary',
            )}
          >
            {isCopied ? (
              <Check className="h-3.5 w-3.5 text-success" aria-hidden="true" />
            ) : (
              <Copy className="h-3.5 w-3.5" aria-hidden="true" />
            )}
          </button>
        </Tooltip>
      )}
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
