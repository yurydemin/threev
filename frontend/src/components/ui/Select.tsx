import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from '@headlessui/react';
import { Check, ChevronDown } from 'lucide-react';
import { cn } from '../../lib/utils';

export interface SelectOption {
  value: string;
  label: string;
}

export interface SelectProps {
  options: SelectOption[];
  value: string;
  onChange: (value: string) => void;
  /** Rendered above the field, matching `Input`'s label styling. */
  label?: string;
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  name?: string;
}

/**
 * Custom dropdown per docs/03-ux-ui-spec.md section 4.4, built on Headless
 * UI's `Listbox` (not a native `<select>`, per the spec's dropdown-panel
 * requirements: shadow, `--bg-elevated`, selected/hover states).
 */
export function Select({
  options,
  value,
  onChange,
  label,
  placeholder = 'Выбрать...',
  disabled,
  className,
  name,
}: SelectProps) {
  const selected = options.find((option) => option.value === value);

  return (
    <Listbox value={value} onChange={onChange} disabled={disabled} name={name}>
      {({ open }) => (
        <div className={cn('flex flex-col', className)}>
          {label && <span className="mb-1 text-xs font-medium text-fg-secondary">{label}</span>}
          <div className="relative">
            <ListboxButton
              className={cn(
                'flex h-8 w-full items-center justify-between rounded border bg-bg-secondary px-2.5 text-left text-[13px] text-fg-primary',
                'transition-colors duration-fast',
                'focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-subtle',
                open ? 'border-accent ring-2 ring-accent-subtle' : 'border-border',
                disabled && 'cursor-not-allowed opacity-50',
              )}
            >
              <span className={cn('truncate', !selected && 'text-fg-muted')}>
                {selected ? selected.label : placeholder}
              </span>
              <ChevronDown className="ml-2 h-4 w-4 shrink-0 text-fg-muted" aria-hidden="true" />
            </ListboxButton>

            <ListboxOptions
              transition
              anchor={{ to: 'bottom start', gap: 4 }}
              className={cn(
                'z-50 max-h-60 w-[var(--button-width)] overflow-y-auto rounded border border-border bg-bg-elevated',
                'shadow-[0_4px_12px_rgba(0,0,0,0.15)] focus:outline-none',
                'transition duration-fast ease-out data-[closed]:scale-95 data-[closed]:opacity-0',
              )}
            >
              {options.map((option) => (
                <ListboxOption
                  key={option.value}
                  value={option.value}
                  className={({ focus, selected: isSelected }) =>
                    cn(
                      'flex cursor-pointer items-center justify-between px-2.5 py-1.5 text-[13px]',
                      focus && 'bg-bg-tertiary',
                      isSelected ? 'bg-accent-subtle text-accent' : 'text-fg-primary',
                    )
                  }
                >
                  {({ selected: isSelected }) => (
                    <>
                      <span className="truncate">{option.label}</span>
                      {isSelected && (
                        <Check className="ml-2 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                      )}
                    </>
                  )}
                </ListboxOption>
              ))}
            </ListboxOptions>
          </div>
        </div>
      )}
    </Listbox>
  );
}
