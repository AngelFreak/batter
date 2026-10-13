'use client';

import type { ReactNode } from 'react';
import type { NavAction } from '@/lib/device-nav';

const BUTTONS: { action: NavAction; label: string; hint: string; icon: ReactNode }[] = [
  {
    action: 'back',
    label: 'Back',
    hint: 'Back (or right-click the screen)',
    icon: <path d="M15 5 7 12l8 7z" />,
  },
  {
    action: 'home',
    label: 'Home',
    hint: 'Home',
    icon: <circle cx="12" cy="12" r="6.5" />,
  },
  {
    action: 'recents',
    label: 'Recents',
    hint: 'Recent apps',
    icon: <rect x="6" y="6" width="12" height="12" rx="2" />,
  },
  {
    action: 'all_apps',
    label: 'All apps',
    hint: 'App drawer',
    icon: (
      <g fill="currentColor" stroke="none">
        {[6.5, 12, 17.5].flatMap((y) =>
          [6.5, 12, 17.5].map((x) => <circle key={`${x}-${y}`} cx={x} cy={y} r="1.6" />),
        )}
      </g>
    ),
  },
];

interface NavBarProps {
  onNav: (action: NavAction) => void;
  disabled?: boolean;
}

/** Android-style navigation buttons shown under a device screen. */
export function NavBar({ onNav, disabled }: NavBarProps) {
  return (
    <div className="flex items-center justify-center gap-1 px-2 py-1 bg-gray-900 border-t border-gray-800 shrink-0">
      {BUTTONS.map(({ action, label, hint, icon }) => (
        <button
          key={action}
          type="button"
          onClick={() => onNav(action)}
          // Keep keyboard focus on the device screen so typing still goes to it.
          onMouseDown={(e) => e.preventDefault()}
          disabled={disabled}
          title={hint}
          aria-label={label}
          className="flex items-center justify-center w-12 h-8 rounded text-gray-400 hover:text-gray-100 hover:bg-gray-800 disabled:opacity-40 disabled:hover:bg-transparent"
        >
          <svg viewBox="0 0 24 24" className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round">
            {icon}
          </svg>
        </button>
      ))}
    </div>
  );
}
