import type { ReactNode } from 'react';
import { AlertTriangle, Info, WifiOff } from 'lucide-react';
import { cx } from './cx';

export interface BannerProps {
  tone?: 'warn' | 'danger' | 'info';
  /** Overrides the default icon for the tone. */
  icon?: ReactNode;
  children: ReactNode;
  /** Buttons on the right. */
  actions?: ReactNode;
  className?: string;
}

/** A full-width notice above content: attention needed, connection lost. */
export function Banner({ tone = 'warn', icon, children, actions, className }: BannerProps) {
  const Icon = tone === 'info' ? Info : tone === 'danger' ? WifiOff : AlertTriangle;
  return (
    <div
      role={tone === 'info' ? 'status' : 'alert'}
      className={cx(
        'flex flex-wrap items-center gap-3 border border-l-2 py-2.5 pl-3.5 pr-3',
        tone === 'warn' && 'border-vx-warn/40 border-l-vx-warn bg-vx-warn/10',
        tone === 'danger' && 'border-vx-danger/40 border-l-vx-danger bg-vx-danger/10',
        tone === 'info' && 'border-vx-hairline border-l-vx-fg-muted bg-vx-elevated',
        className,
      )}
    >
      <span
        className={cx(
          'shrink-0',
          tone === 'warn' && 'text-vx-warn',
          tone === 'danger' && 'text-vx-danger',
          tone === 'info' && 'text-vx-fg-muted',
        )}
        aria-hidden
      >
        {icon ?? <Icon size={16} />}
      </span>
      <div className="min-w-0 flex-1 text-sm text-vx-fg">{children}</div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}
