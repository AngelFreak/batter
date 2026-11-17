'use client';

import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { getGroupDevices, DeviceGroup, DeviceInfo } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { cx, useToast } from '@/components/ui';
import { GroupDevicesTab } from './group-devices-tab';
import { GroupAccessTab } from './group-access-tab';
import { GroupSettingsTab } from './group-settings-tab';

type Tab = 'devices' | 'access' | 'settings';

const TABS: { id: Tab; label: string; adminOnly: boolean }[] = [
  { id: 'devices', label: 'Phones', adminOnly: false },
  { id: 'access', label: 'Access', adminOnly: true },
  { id: 'settings', label: 'Settings', adminOnly: true },
];

interface Props {
  /** Id of the panel element, so the row's toggle can point at it. */
  panelId: string;
  group: DeviceGroup;
  devices: DeviceInfo[];
  isAdmin: boolean;
  /** Reload the group list and devices in the page. */
  onChanged: () => void;
  onDeleted: () => void;
}

/** The expanded part of a group: its phones, and for admins its access and settings. */
export function GroupPanel({ panelId, group, devices, isAdmin, onChanged, onDeleted }: Props) {
  const toast = useToast();
  const [tab, setTab] = useState<Tab>('devices');
  const [members, setMembers] = useState<string[] | null>(null);
  const tabRefs = useRef<Partial<Record<Tab, HTMLButtonElement | null>>>({});

  const loadMembers = useCallback(async () => {
    try {
      setMembers(await getGroupDevices(group.id));
    } catch (err) {
      setMembers(prev => prev ?? []);
      toast.error(errorMessage(err, 'load the phones in this group'));
    }
  }, [group.id, toast]);

  useEffect(() => {
    loadMembers();
    // Only when the group changes; `toast` is stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [group.id]);

  const visible = TABS.filter(t => isAdmin || !t.adminOnly);

  const changed = async () => {
    await loadMembers();
    onChanged();
  };

  // Left and right move between tabs, as on any tab list.
  const onTabKey = (e: KeyboardEvent, index: number) => {
    let next = index;
    if (e.key === 'ArrowRight') next = (index + 1) % visible.length;
    else if (e.key === 'ArrowLeft') next = (index - 1 + visible.length) % visible.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = visible.length - 1;
    else return;
    e.preventDefault();
    const id = visible[next].id;
    setTab(id);
    tabRefs.current[id]?.focus();
  };

  return (
    <div id={panelId} className="border-t border-vx-hairline bg-vx-recessed">
      {visible.length > 1 && (
        <div role="tablist" aria-label={`${group.name} sections`} className="flex border-b border-vx-hairline px-2 sm:px-4">
          {visible.map((t, i) => (
            <button
              key={t.id}
              ref={el => {
                tabRefs.current[t.id] = el;
              }}
              type="button"
              role="tab"
              id={`${panelId}-tab-${t.id}`}
              aria-selected={tab === t.id}
              aria-controls={`${panelId}-body`}
              tabIndex={tab === t.id ? 0 : -1}
              onClick={() => setTab(t.id)}
              onKeyDown={e => onTabKey(e, i)}
              className={cx(
                'min-h-touch border-b-2 px-3 font-mono text-xs uppercase tracking-wide transition-colors',
                tab === t.id
                  ? 'border-vx-accent text-vx-accent'
                  : 'border-transparent text-vx-fg-muted hover:text-vx-fg',
              )}
            >
              {t.label}
            </button>
          ))}
        </div>
      )}
      <div
        id={`${panelId}-body`}
        role={visible.length > 1 ? 'tabpanel' : undefined}
        aria-labelledby={visible.length > 1 ? `${panelId}-tab-${tab}` : undefined}
        className="p-4 sm:p-5"
      >
        {tab === 'devices' && (
          <GroupDevicesTab group={group} members={members} devices={devices} isAdmin={isAdmin} onChanged={changed} />
        )}
        {tab === 'access' && isAdmin && <GroupAccessTab group={group} />}
        {tab === 'settings' && isAdmin && <GroupSettingsTab group={group} onSaved={onChanged} onDeleted={onDeleted} />}
      </div>
    </div>
  );
}
