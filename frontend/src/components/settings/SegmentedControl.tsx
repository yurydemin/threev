import { cn } from '../../lib/utils';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
}

export interface SegmentedControlProps<T extends string> {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
}

/**
 * Small "pick exactly one of N fixed values" control, styled as a row of
 * segmented buttons (no dedicated Radio primitive exists yet in
 * `components/ui/*` — see `PresignedUrlModal`'s equivalent note about the
 * missing Slider primitive). Shared by `AppearanceSection` (theme, UI
 * scale) and `TransfersSection` (part-size override) — every one of their
 * radio-style choices is a small, closed set of fixed values, not free
 * text, so a single generic component covers all three instead of three
 * near-identical bespoke button rows.
 */
export function SegmentedControl<T extends string>({ options, value, onChange }: SegmentedControlProps<T>) {
  return (
    <div className="inline-flex overflow-hidden rounded border border-border">
      {options.map((option, index) => {
        const active = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(option.value)}
            className={cn(
              'px-3 py-1.5 text-[13px] transition-colors duration-fast',
              index > 0 && 'border-l border-border',
              active ? 'bg-accent-subtle text-accent' : 'bg-bg-secondary text-fg-secondary hover:bg-bg-tertiary',
            )}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
