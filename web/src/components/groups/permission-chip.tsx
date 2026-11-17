import { cx } from '@/components/ui';

/** The permission word on an access grant. Always the word, never colour alone. */
export function PermissionChip({ permission }: { permission: string }) {
  return (
    <span
      className={cx(
        'inline-flex items-center whitespace-nowrap rounded-sm border px-1.5 py-0.5 font-mono text-[11px] uppercase leading-none tracking-wide',
        permission === 'manage' ? 'border-vx-warn/50 text-vx-warn' : 'border-vx-control text-vx-fg-muted',
      )}
    >
      {permission}
    </span>
  );
}
