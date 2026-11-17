'use client';

import type { ReactNode } from 'react';
import { cx } from './cx';

export interface TileProps {
  label: ReactNode;
  value: ReactNode;
  /** A line under the number. */
  sub?: ReactNode;
  /** When set, the tile is a toggle button (e.g. a status filter). */
  pressed?: boolean;
  onClick?: () => void;
  className?: string;
}

/**
 * A number with a label. Lay tiles out in a grid with `gap-px bg-vx-hairline`
 * for hairline dividers.
 */
export function Tile({ label, value, sub, pressed, onClick, className }: TileProps) {
  const body = (
    <>
      <span className="flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-wide">{label}</span>
      <span className="text-[30px] font-extrabold leading-none tracking-headline tabular-nums">{value}</span>
      {sub && <span className="text-[12.5px] leading-tight text-vx-fg-muted">{sub}</span>}
    </>
  );
  const base = cx(
    'relative flex min-h-[86px] flex-col items-start gap-1.5 bg-vx-surface px-3.5 pb-3 pt-3.5 text-left text-vx-fg',
    className,
  );
  if (!onClick) return <div className={base}>{body}</div>;
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cx(
        base,
        'transition-colors hover:bg-vx-elevated focus-visible:outline-offset-[-2px]',
        pressed && 'bg-vx-elevated after:absolute after:inset-x-0 after:top-0 after:h-0.5 after:bg-vx-accent',
      )}
    >
      {body}
    </button>
  );
}
