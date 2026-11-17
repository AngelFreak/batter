import type { StatusIcon, StatusTone, StatusView } from '@/lib/device-status';
import { cx } from './cx';

const TONE: Record<StatusTone, string> = {
  live: 'text-vx-accent',
  idle: 'text-vx-fg-muted',
  muted: 'text-vx-fg-label',
  warn: 'text-vx-warn',
  danger: 'text-vx-danger',
};

/** The shape beside a status word, so it reads without colour. */
export function StatusGlyph({ icon, className }: { icon: StatusIcon; className?: string }) {
  return (
    <svg viewBox="0 0 14 14" aria-hidden="true" className={cx('h-3.5 w-3.5 shrink-0', className)}>
      {icon === 'dot' && <circle cx="7" cy="7" r="3.5" fill="currentColor" />}
      {icon === 'ring' && <circle cx="7" cy="7" r="3.75" fill="none" stroke="currentColor" strokeWidth="1.5" />}
      {icon === 'slash' && (
        <>
          <circle cx="7" cy="7" r="4.75" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="M3.5 10.5 10.5 3.5" stroke="currentColor" strokeWidth="1.5" />
        </>
      )}
      {icon === 'triangle' && <path d="M7 2 12.5 12h-11Z" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="miter" />}
      {icon === 'square' && <rect x="3" y="3" width="8" height="8" fill="currentColor" />}
    </svg>
  );
}

export interface StatusChipProps {
  status: StatusView;
  /** Show the hint as a tooltip and to screen readers. Default true. */
  withHint?: boolean;
  className?: string;
}

/** Icon plus word. Status is never shown as colour alone. */
export function StatusChip({ status, withHint = true, className }: StatusChipProps) {
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1.5 whitespace-nowrap font-mono text-[11px] uppercase leading-none tracking-wide',
        TONE[status.tone],
        className,
      )}
      title={withHint && status.hint ? status.hint : undefined}
    >
      <StatusGlyph icon={status.icon} className={status.tone === 'live' ? 'vx-pulse' : undefined} />
      {status.label}
      {withHint && status.hint && <span className="sr-only">: {status.hint}</span>}
    </span>
  );
}
