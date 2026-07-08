import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useRef,
  type InputHTMLAttributes,
} from 'react';
import { Check, Minus } from 'lucide-react';
import { cn } from '../../lib/utils';

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'size'> {
  /** Rendered to the right of the box. */
  label?: string;
  /**
   * Visual "partial selection" state. Native `<input>` doesn't expose this
   * as an attribute, so it's applied imperatively via a ref effect.
   */
  indeterminate?: boolean;
}

/**
 * Checkbox per docs/03-ux-ui-spec.md section 4.5.
 *
 * Implemented as a real `<input type="checkbox">` (kept focusable/keyboard
 * operable) visually replaced by a styled sibling `<span>`; the native
 * input is layered underneath with `pointer-events` passed through so
 * clicks and focus still land on it.
 */
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(
  { label, indeterminate = false, className, id, checked, disabled, ...props },
  ref,
) {
  const inputRef = useRef<HTMLInputElement>(null);
  useImperativeHandle(ref, () => inputRef.current as HTMLInputElement);

  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.indeterminate = indeterminate;
    }
  }, [indeterminate]);

  const generatedId = useId();
  const checkboxId = id ?? generatedId;
  const showAccent = !!checked || indeterminate;

  return (
    <label
      htmlFor={checkboxId}
      className={cn(
        'group inline-flex items-center gap-2 text-[13px] text-fg-primary',
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
        className,
      )}
    >
      <span className="relative inline-flex h-4 w-4 shrink-0">
        <input
          ref={inputRef}
          id={checkboxId}
          type="checkbox"
          checked={checked}
          disabled={disabled}
          className="peer absolute inset-0 h-4 w-4 cursor-pointer appearance-none disabled:cursor-not-allowed"
          {...props}
        />
        <span
          aria-hidden="true"
          className={cn(
            'pointer-events-none absolute inset-0 flex items-center justify-center rounded-sm border transition-colors duration-fast',
            'peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent',
            showAccent
              ? 'border-accent bg-accent'
              : 'border-border bg-bg-secondary group-hover:border-accent',
          )}
        >
          {indeterminate ? (
            <Minus className="h-2.5 w-2.5 text-white" strokeWidth={3} />
          ) : showAccent ? (
            <Check className="h-3 w-3 text-white" strokeWidth={3} />
          ) : null}
        </span>
      </span>
      {label && <span>{label}</span>}
    </label>
  );
});
