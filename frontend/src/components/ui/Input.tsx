import { forwardRef, useId, type InputHTMLAttributes } from 'react';
import { cn } from '../../lib/utils';

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  /** Rendered above the field (12px, weight 500, `--fg-secondary`), per section 5.3. */
  label?: string;
  /** Validation message. When set, the field switches to the error visual state. */
  error?: string;
}

/**
 * Text field per docs/03-ux-ui-spec.md section 4.3.
 */
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, className, id, ...props },
  ref,
) {
  const generatedId = useId();
  const inputId = id ?? generatedId;

  return (
    <div className="flex flex-col">
      {label && (
        <label htmlFor={inputId} className="mb-1 text-xs font-medium text-fg-secondary">
          {label}
        </label>
      )}
      <input
        ref={ref}
        id={inputId}
        className={cn(
          'h-8 w-full rounded border bg-bg-secondary px-2.5 text-[13px] text-fg-primary',
          'placeholder:text-fg-muted',
          'transition-colors duration-fast',
          'focus:outline-none focus-visible:ring-2',
          error
            ? 'border-danger focus:border-danger focus-visible:ring-danger-subtle'
            : 'border-border focus:border-accent focus-visible:ring-accent-subtle',
          'disabled:cursor-not-allowed disabled:opacity-50',
          className,
        )}
        aria-invalid={!!error || undefined}
        aria-describedby={error ? `${inputId}-error` : undefined}
        {...props}
      />
      {error && (
        <p id={`${inputId}-error`} className="mt-1 text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  );
});
