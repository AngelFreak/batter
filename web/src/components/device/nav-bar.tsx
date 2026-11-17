'use client';

import type { ReactNode } from 'react';
import { ChevronLeft, Circle, Grid3x3, Square } from 'lucide-react';
import type { NavAction } from '@/lib/device-nav';

const BUTTONS: { action: NavAction; label: string; hint: string; icon: ReactNode }[] = [
  { action: 'back', label: 'Back', hint: 'Back (or right-click the screen)', icon: <ChevronLeft size={18} /> },
  { action: 'home', label: 'Home', hint: 'Home', icon: <Circle size={16} /> },
  { action: 'recents', label: 'Recents', hint: 'Recent apps', icon: <Square size={16} /> },
  { action: 'all_apps', label: 'All apps', hint: 'App drawer', icon: <Grid3x3 size={16} /> },
];

interface NavBarProps {
  onNav: (action: NavAction) => void;
  disabled?: boolean;
  /** Why the buttons are disabled; shown as their title. */
  disabledReason?: string;
}

/** Android-style navigation buttons shown under a device screen. */
export function NavBar({ onNav, disabled, disabledReason }: NavBarProps) {
  return (
    <div role="group" aria-label="Android navigation" className="shrink-0 border-t border-vx-hairline bg-vx-surface">
      <div className="mx-auto grid max-w-md grid-cols-4">
        {BUTTONS.map(({ action, label, hint, icon }) => (
          <button
            key={action}
            type="button"
            onClick={() => onNav(action)}
            // Keep keyboard focus on the device screen so typing still goes to it.
            onMouseDown={(e) => e.preventDefault()}
            disabled={disabled}
            title={disabled && disabledReason ? disabledReason : hint}
            aria-label={label}
            className="flex min-h-touch flex-col items-center justify-center gap-1 border-r border-vx-hairline py-1.5 font-mono text-[11px] uppercase tracking-wide text-vx-fg-muted transition-colors last:border-r-0 hover:bg-vx-elevated hover:text-vx-fg disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-vx-fg-muted focus-visible:[outline-offset:-2px]"
          >
            {icon}
            <span aria-hidden>{label}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
