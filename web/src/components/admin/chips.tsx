import type { ReactNode } from 'react';
import { cx } from '@/components/ui';

/** A small mono tag. The word carries the meaning, never the colour. */
export function Chip({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span
      className={cx(
        'inline-flex items-center whitespace-nowrap rounded-sm border px-1.5 py-1 font-mono text-[11px] uppercase leading-none tracking-wide',
        className,
      )}
    >
      {children}
    </span>
  );
}

const ROLE_TONE: Record<string, string> = {
  admin: 'border-vx-fg-muted text-vx-fg',
  operator: 'border-vx-control text-vx-fg-muted',
  viewer: 'border-vx-hairline text-vx-fg-label',
};

export function RoleChip({ role }: { role: string }) {
  return <Chip className={ROLE_TONE[role] ?? ROLE_TONE.viewer}>{role}</Chip>;
}

const PERMISSION_TONE: Record<string, string> = {
  manage: 'border-vx-fg-muted text-vx-fg',
  control: 'border-vx-control text-vx-fg-muted',
  view: 'border-vx-hairline text-vx-fg-label',
};

export function PermissionChip({ permission }: { permission: string }) {
  return <Chip className={PERMISSION_TONE[permission] ?? PERMISSION_TONE.view}>{permission}</Chip>;
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return name.substring(0, 2).toUpperCase();
}

/** Square initials in mono, one neutral tone for everyone. */
export function Monogram({ name }: { name: string }) {
  return (
    <span
      aria-hidden
      className="grid h-9 w-9 shrink-0 place-items-center rounded-sm border border-vx-hairline bg-vx-elevated font-mono text-xs text-vx-fg-muted"
    >
      {initials(name)}
    </span>
  );
}
