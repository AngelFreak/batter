'use client';

import { useEffect, useRef, useState } from 'react';
import { usePathname } from 'next/navigation';
import { Menu as MenuIcon, X } from 'lucide-react';
import { Sidebar, Logomark } from './sidebar';
import { Button, ToastProvider } from '@/components/ui';

const NO_SIDEBAR_ROUTES = ['/login', '/setup'];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const showSidebar = !NO_SIDEBAR_ROUTES.some((r) => pathname.startsWith(r));

  return <ToastProvider>{showSidebar ? <Shell>{children}</Shell> : children}</ToastProvider>;
}

function Shell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const drawer = useRef<HTMLDivElement>(null);

  // Navigating closes the drawer.
  useEffect(() => setDrawerOpen(false), [pathname]);

  useEffect(() => {
    if (!drawerOpen) return;
    drawer.current?.querySelector<HTMLElement>('a, button')?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setDrawerOpen(false);
        menuButton.current?.focus();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [drawerOpen]);

  return (
    <div className="flex h-dvh flex-col nav:flex-row">
      {/* Wide screens: fixed sidebar. */}
      <div className="hidden nav:block">
        <Sidebar />
      </div>

      {/* Narrow screens: top bar with a menu button, sidebar as a drawer. */}
      <div className="flex h-14 shrink-0 items-center gap-3 border-b border-vx-hairline bg-vx-recessed px-4 nav:hidden">
        <Button
          ref={menuButton}
          variant="quiet"
          iconOnly
          size="lg"
          className="-ml-2"
          aria-label="Open menu"
          aria-expanded={drawerOpen}
          aria-controls="vx-drawer"
          icon={<MenuIcon size={20} />}
          onClick={() => setDrawerOpen(true)}
        />
        <Logomark className="h-5 w-5" />
        <span className="text-base font-extrabold tracking-wide">Batter</span>
      </div>
      {drawerOpen && (
        <div className="fixed inset-0 z-40 nav:hidden">
          <button
            type="button"
            aria-label="Close menu"
            tabIndex={-1}
            className="absolute inset-0 bg-vx-recessed/80"
            onClick={() => setDrawerOpen(false)}
          />
          <div ref={drawer} id="vx-drawer" role="dialog" aria-modal="true" aria-label="Menu" className="relative h-full w-[232px]">
            <Sidebar onNavigate={() => setDrawerOpen(false)} />
            <Button
              variant="quiet"
              iconOnly
              size="sm"
              className="absolute right-2 top-3"
              aria-label="Close menu"
              icon={<X size={16} />}
              onClick={() => {
                setDrawerOpen(false);
                menuButton.current?.focus();
              }}
            />
          </div>
        </div>
      )}

      <main className="min-h-0 min-w-0 flex-1 overflow-auto">{children}</main>
    </div>
  );
}
