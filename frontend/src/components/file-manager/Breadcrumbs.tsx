import { Fragment } from 'react';

export interface BreadcrumbsProps {
  bucket: string;
  /** S3-style prefix, e.g. `"folder1/folder2/"` (empty string = bucket root). */
  prefix: string;
  onNavigate: (prefix: string) => void;
}

/**
 * Breadcrumb trail per docs/03-ux-ui-spec.md section 5.4.1: bucket name
 * followed by one clickable segment per path component of `prefix`.
 * Clicking the bucket name navigates to the root (`prefix=''`); clicking
 * segment N navigates to the prefix built from segments `0..N` inclusive.
 */
export function Breadcrumbs({ bucket, prefix, onNavigate }: BreadcrumbsProps) {
  const segments = prefix.split('/').filter(Boolean);

  return (
    <nav className="flex min-w-0 items-center gap-1.5 overflow-hidden" aria-label="Breadcrumb">
      <BreadcrumbSegment label={bucket} onClick={() => onNavigate('')} />
      {segments.map((segment, index) => (
        <Fragment key={index}>
          <span className="shrink-0 text-xs text-fg-muted">/</span>
          <BreadcrumbSegment
            label={segment}
            onClick={() => onNavigate(`${segments.slice(0, index + 1).join('/')}/`)}
          />
        </Fragment>
      ))}
    </nav>
  );
}

function BreadcrumbSegment({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      className="min-w-0 shrink truncate text-[13px] text-fg-primary transition-colors duration-fast hover:text-accent"
    >
      {label}
    </button>
  );
}
