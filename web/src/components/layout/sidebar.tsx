'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { Smartphone, FolderTree, LayoutGrid, Keyboard, User, Users, ShieldCheck, Cable, LogOut, type LucideIcon } from 'lucide-react';
import { logout } from '@/lib/api';
import { useUser } from '@/lib/use-user';
import { Button, cx } from '@/components/ui';

interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
  /** One line saying what the page is for, where the name alone doesn't. */
  hint?: string;
}

export const navItems: NavItem[] = [
  { href: '/dashboard', label: 'Devices', icon: Smartphone },
  { href: '/groups', label: 'Groups', icon: FolderTree },
  { href: '/multiview', label: 'Multi View', icon: LayoutGrid, hint: 'Watch many phones' },
  { href: '/multiplexer', label: 'Multiplexer', icon: Keyboard, hint: 'Type into many phones' },
];

export const adminItems: NavItem[] = [
  { href: '/admin/users', label: 'Users', icon: User },
  { href: '/admin/user-groups', label: 'Teams', icon: Users },
  { href: '/admin/vpn', label: 'VPN', icon: ShieldCheck },
  { href: '/admin/phone-network', label: 'Phone network', icon: Cable },
];

/** The VesperX mark: an outlined diamond around a solid one. */
export function Logomark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={cx('h-6 w-6 shrink-0', className)}>
      <rect x="5.5" y="5.5" width="13" height="13" transform="rotate(45 12 12)" fill="none" stroke="rgb(var(--vx-accent))" strokeWidth="1.5" />
      <rect x="9.9" y="9.9" width="4.2" height="4.2" transform="rotate(45 12 12)" fill="rgb(var(--vx-accent))" />
    </svg>
  );
}

function NavLink({ item, active, onNavigate }: { item: NavItem; active: boolean; onNavigate?: () => void }) {
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      onClick={onNavigate}
      aria-current={active ? 'page' : undefined}
      className={cx(
        'relative flex min-h-[40px] items-center gap-3 px-2.5 py-1.5 transition-colors',
        'hover:bg-vx-elevated hover:text-vx-fg',
        active ? 'bg-vx-elevated text-vx-fg before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:bg-vx-accent' : 'text-vx-fg-muted',
      )}
    >
      <Icon size={16} strokeWidth={1.5} aria-hidden className="shrink-0" />
      <span className="min-w-0">
        <span className="block font-mono text-xs uppercase tracking-wide">{item.label}</span>
        {item.hint && <span className="block text-[12px] leading-tight text-vx-fg-label">{item.hint}</span>}
      </span>
    </Link>
  );
}

export function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const router = useRouter();
  const { user, ready, isAdmin } = useUser();
  const isActive = (href: string) => pathname === href || pathname.startsWith(href + '/');

  const handleLogout = async () => {
    await logout();
    router.push('/login');
  };

  const name = user?.display_name || user?.username || '';
  return (
    <div className="flex h-full w-[232px] flex-col border-r border-vx-hairline bg-vx-recessed px-3 pb-3 pt-5">
      <div className="flex items-center gap-3 px-2">
        <Logomark />
        <div className="leading-none">
          <span className="block text-lg font-extrabold tracking-wide text-vx-fg">Batter</span>
          <span className="mt-1.5 block font-mono text-[11px] uppercase tracking-label text-vx-fg-label">Phone control</span>
        </div>
      </div>

      <nav aria-label="Main" className="mt-7 flex flex-1 flex-col gap-px overflow-y-auto">
        {navItems.map((item) => (
          <NavLink key={item.href} item={item} active={isActive(item.href)} onNavigate={onNavigate} />
        ))}
        {ready && isAdmin && (
          <>
            <p className="vx-label px-2.5 pb-2 pt-5">Admin</p>
            {adminItems.map((item) => (
              <NavLink key={item.href} item={item} active={isActive(item.href)} onNavigate={onNavigate} />
            ))}
          </>
        )}
      </nav>

      {ready && user && (
        <div className="mt-4 flex items-center gap-2.5 border-t border-vx-hairline px-2 pt-3">
          <span
            aria-hidden
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center border border-vx-control bg-vx-elevated font-mono text-xs font-semibold uppercase"
          >
            {name.slice(0, 2)}
          </span>
          <div className="min-w-0 flex-1 leading-tight">
            <span className="block truncate text-sm font-semibold">{name}</span>
            <span className="font-mono text-[11px] uppercase tracking-wide text-vx-fg-label">{user.role}</span>
          </div>
          <Button variant="quiet" size="sm" iconOnly aria-label="Sign out" title="Sign out" icon={<LogOut size={15} />} onClick={handleLogout} />
        </div>
      )}
    </div>
  );
}
