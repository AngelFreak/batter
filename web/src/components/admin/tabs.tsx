'use client';

import { useId, type KeyboardEvent } from 'react';
import { cx } from '@/components/ui';

export interface TabItem<K extends string> {
  key: K;
  label: string;
}

/**
 * A tab strip. Arrow keys, Home and End move between tabs. Render the panel
 * with `tabPanelProps(ids, key)` so the two stay linked.
 */
export function useTabIds() {
  const base = useId();
  return {
    tab: (k: string) => `${base}-tab-${k}`,
    panel: (k: string) => `${base}-panel-${k}`,
  };
}

export type TabIds = ReturnType<typeof useTabIds>;

export function tabPanelProps(ids: TabIds, key: string) {
  return { role: 'tabpanel' as const, id: ids.panel(key), 'aria-labelledby': ids.tab(key), tabIndex: 0 };
}

export function Tabs<K extends string>({
  label,
  tabs,
  value,
  onChange,
  ids,
}: {
  label: string;
  tabs: TabItem<K>[];
  value: K;
  onChange: (k: K) => void;
  ids: TabIds;
}) {
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = tabs.findIndex((t) => t.key === value);
    let next = -1;
    if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    if (next < 0) return;
    e.preventDefault();
    onChange(tabs[next].key);
    requestAnimationFrame(() => document.getElementById(ids.tab(tabs[next].key))?.focus());
  };

  return (
    <div role="tablist" aria-label={label} onKeyDown={onKey} className="flex overflow-x-auto border-b border-vx-hairline">
      {tabs.map((t) => {
        const selected = t.key === value;
        return (
          <button
            key={t.key}
            type="button"
            role="tab"
            id={ids.tab(t.key)}
            aria-selected={selected}
            aria-controls={ids.panel(t.key)}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(t.key)}
            className={cx(
              '-mb-px h-touch shrink-0 border-b-2 px-4 font-mono text-xs uppercase tracking-wide transition-colors sm:h-10',
              selected ? 'border-vx-accent text-vx-fg' : 'border-transparent text-vx-fg-muted hover:text-vx-fg',
            )}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}
