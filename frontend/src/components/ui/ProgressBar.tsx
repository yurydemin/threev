import { cn } from '../../lib/utils';

export interface ProgressBarProps {
  /** Percentage in `[0, 100]`; values outside that range are clamped. */
  value: number;
  /** `upload` renders `--accent`, `download` renders `--success` (UX spec 5.5). */
  variant: 'upload' | 'download';
  className?: string;
}

const VARIANT_FILL_CLASSES: Record<ProgressBarProps['variant'], string> = {
  upload: 'bg-accent',
  download: 'bg-success',
};

/**
 * Transfer progress bar per docs/03-ux-ui-spec.md section 5.5: 8px height,
 * rounded track, `--accent` for uploads / `--success` for downloads, width
 * transition animates over 300ms so progress ticks feel continuous rather
 * than jumpy.
 */
export function ProgressBar({ value, variant, className }: ProgressBarProps) {
  const clamped = Math.min(100, Math.max(0, value));

  return (
    <div
      className={cn('h-2 w-full overflow-hidden rounded-full bg-bg-tertiary', className)}
      role="progressbar"
      aria-valuenow={Math.round(clamped)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div
        className={cn('h-full rounded-full transition-[width] duration-300 ease-out', VARIANT_FILL_CLASSES[variant])}
        style={{ width: `${clamped}%` }}
      />
    </div>
  );
}
