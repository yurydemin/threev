import { forwardRef, type ButtonHTMLAttributes } from 'react';
import { Loader2 } from 'lucide-react';
import { cn } from '../../lib/utils';

export type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost';
export type ButtonSize = 'default' | 'large';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** Visual style. Defaults to `primary`. */
  variant?: ButtonVariant;
  /** `large` is used for prominent CTAs (e.g. Welcome screen). */
  size?: ButtonSize;
  /** Renders a 32x32 square button (`ICON_SIZE_LARGE` icon slot), no label padding. */
  iconOnly?: boolean;
  /** Shows a spinner in place of the label while keeping layout width stable. */
  isLoading?: boolean;
}

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-white hover:bg-accent-hover',
  secondary: 'bg-bg-secondary text-fg-primary border border-border hover:bg-bg-tertiary',
  danger: 'bg-danger text-white hover:bg-danger-hover',
  ghost: 'bg-transparent text-fg-secondary hover:bg-bg-tertiary',
};

/**
 * Base button per docs/03-ux-ui-spec.md section 4.2.
 *
 * `iconOnly` takes precedence over `size` (icon buttons are always 32x32,
 * `rounded-sm`, regardless of `size`).
 */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = 'primary',
    size = 'default',
    iconOnly = false,
    isLoading = false,
    disabled,
    type = 'button',
    className,
    children,
    ...props
  },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      disabled={disabled || isLoading}
      aria-busy={isLoading || undefined}
      className={cn(
        'relative inline-flex items-center justify-center gap-1.5 font-medium',
        'transition-[background-color,transform,opacity] duration-fast ease-out',
        'active:scale-[0.97]',
        'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        'disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50',
        VARIANT_CLASSES[variant],
        iconOnly
          ? 'h-8 w-8 shrink-0 rounded-sm p-0'
          : cn(
              'rounded',
              size === 'large' ? 'px-5 py-2.5 text-sm' : 'px-3 py-1.5 text-[13px]',
            ),
        className,
      )}
      {...props}
    >
      <span className={cn('inline-flex items-center justify-center gap-1.5', isLoading && 'invisible')}>
        {children}
      </span>
      {isLoading && (
        <Loader2
          className="absolute h-4 w-4 animate-spin"
          aria-hidden="true"
        />
      )}
    </button>
  );
});
