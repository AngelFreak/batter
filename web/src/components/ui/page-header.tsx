import type { ReactNode } from 'react';

export interface PageHeaderProps {
  title: ReactNode;
  /** A line under the title: counts, context. */
  sub?: ReactNode;
  /** Buttons on the right; they wrap under the title on narrow screens. */
  actions?: ReactNode;
  /** Small mono label above the title, e.g. "ADMIN". */
  eyebrow?: string;
}

export function PageHeader({ title, sub, actions, eyebrow }: PageHeaderProps) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4 border-b border-vx-hairline px-4 pb-5 pt-6 sm:px-6">
      <div className="min-w-0">
        {eyebrow && <p className="vx-label mb-2">{eyebrow}</p>}
        <h1 className="text-[26px] leading-tight sm:text-[30px]">{title}</h1>
        {sub && <div className="mt-2 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[13.5px] text-vx-fg-muted">{sub}</div>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}
