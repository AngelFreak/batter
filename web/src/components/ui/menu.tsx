'use client';

import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { cx } from './cx';

export interface MenuItem {
  label: string;
  icon?: ReactNode;
  onSelect: () => void;
  danger?: boolean;
  disabled?: boolean;
  /** Why it is disabled; shown as the item's tooltip. */
  disabledReason?: string;
}

export interface MenuProps {
  /** Renders the trigger. Spread `props` onto a Button. */
  trigger: (props: {
    ref: React.Ref<HTMLButtonElement>;
    onClick: () => void;
    onKeyDown: (e: React.KeyboardEvent) => void;
    'aria-haspopup': 'menu';
    'aria-expanded': boolean;
    'aria-controls': string;
  }) => ReactNode;
  items: (MenuItem | 'separator')[];
  align?: 'start' | 'end';
  /** Open upwards (for bottom bars). */
  side?: 'bottom' | 'top';
}

/**
 * A button that opens a list of actions. Opens with click, Enter, Space or
 * ArrowDown; arrows move, Esc closes and returns focus to the trigger.
 */
export function Menu({ trigger, items, align = 'end', side = 'bottom' }: MenuProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const focusItem = (i: number) => {
    const els = listRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])');
    if (!els || els.length === 0) return;
    els[(i + els.length) % els.length].focus();
  };

  useEffect(() => {
    if (!open) return;
    focusItem(0);
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (!listRef.current?.contains(t) && !triggerRef.current?.contains(t)) setOpen(false);
    };
    document.addEventListener('pointerdown', onDown);
    return () => document.removeEventListener('pointerdown', onDown);
  }, [open]);

  const close = (refocus = true) => {
    setOpen(false);
    if (refocus) triggerRef.current?.focus();
  };

  const onListKey = (e: React.KeyboardEvent) => {
    const els = Array.from(listRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ?? []);
    const i = els.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      focusItem(i + 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      focusItem(i - 1);
    } else if (e.key === 'Home') {
      e.preventDefault();
      focusItem(0);
    } else if (e.key === 'End') {
      e.preventDefault();
      focusItem(els.length - 1);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if (e.key === 'Tab') {
      close(false);
    }
  };

  return (
    <div className="relative inline-flex">
      {trigger({
        ref: triggerRef,
        onClick: () => setOpen((o) => !o),
        onKeyDown: (e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault();
            setOpen(true);
          }
        },
        'aria-haspopup': 'menu',
        'aria-expanded': open,
        'aria-controls': id,
      })}
      {open && (
        <div
          ref={listRef}
          id={id}
          role="menu"
          onKeyDown={onListKey}
          className={cx(
            'absolute z-40 min-w-[200px] max-w-[min(320px,calc(100vw-32px))] border border-vx-control bg-vx-elevated py-1',
            align === 'end' ? 'right-0' : 'left-0',
            side === 'bottom' ? 'top-full mt-1' : 'bottom-full mb-1',
          )}
        >
          {items.map((item, i) =>
            item === 'separator' ? (
              <div key={`sep-${i}`} role="separator" className="my-1 border-t border-vx-hairline" />
            ) : (
              <button
                key={item.label}
                type="button"
                role="menuitem"
                disabled={item.disabled}
                title={item.disabled ? item.disabledReason : undefined}
                onClick={() => {
                  close();
                  item.onSelect();
                }}
                className={cx(
                  'flex min-h-touch w-full items-center gap-3 px-3.5 text-left text-sm',
                  'hover:bg-vx-surface focus:bg-vx-surface focus:outline-none focus-visible:outline-none',
                  'disabled:cursor-not-allowed disabled:opacity-50',
                  item.danger ? 'text-vx-danger' : 'text-vx-fg',
                )}
              >
                {item.icon && <span className="shrink-0 text-current [&>svg]:h-4 [&>svg]:w-4">{item.icon}</span>}
                <span className="min-w-0 flex-1">{item.label}</span>
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}
