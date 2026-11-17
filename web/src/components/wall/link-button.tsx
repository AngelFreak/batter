import Link from 'next/link';
import type { ReactNode } from 'react';
import { cx } from '@/components/ui';

/** A link that looks like the secondary Button, for navigation actions. */
export function LinkButton({
  href,
  children,
  className,
}: {
  href: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Link
      href={href}
      className={cx(
        'inline-flex h-9 items-center justify-center gap-2 whitespace-nowrap rounded-sm border border-vx-control px-3.5',
        'font-mono text-xs font-medium uppercase tracking-wide text-vx-fg transition-colors',
        'hover:border-vx-accent hover:text-vx-accent',
        className,
      )}
    >
      {children}
    </Link>
  );
}
