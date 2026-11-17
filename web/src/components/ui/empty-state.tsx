import type { ReactNode } from 'react';

export interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  /** Why it's empty and what to do. */
  children?: ReactNode;
  action?: ReactNode;
}

/** What to show instead of an empty list: why, and what to do next. */
export function EmptyState({ icon, title, children, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-start gap-3 border border-dashed border-vx-hairline px-6 py-10 sm:items-center sm:text-center">
      {icon && <span className="text-vx-fg-label [&>svg]:h-6 [&>svg]:w-6">{icon}</span>}
      <h2 className="text-lg">{title}</h2>
      {children && <div className="max-w-prose text-sm text-vx-fg-muted">{children}</div>}
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
