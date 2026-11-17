'use client';

import { useState } from 'react';
import { Plus, X } from 'lucide-react';
import { addDevicesToGroup, removeDeviceFromGroup, DeviceGroup, DeviceInfo } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { displayName } from '@/lib/dashboard-filter';
import { deviceStatus } from '@/lib/device-status';
import { Button, ConfirmDialog, Field, StatusChip, useToast } from '@/components/ui';

interface Props {
  group: DeviceGroup;
  /** Member serials; null while loading. */
  members: string[] | null;
  devices: DeviceInfo[];
  isAdmin: boolean;
  /** Reload the members and the group list after a change. */
  onChanged: () => Promise<void>;
}

export function GroupDevicesTab({ group, members, devices, isAdmin, onChanged }: Props) {
  const toast = useToast();
  const [search, setSearch] = useState('');
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);

  const info = (serial: string) => devices.find(d => d.serial === serial);
  const nameOf = (serial: string) => {
    const d = info(serial);
    return d ? displayName(d) : serial;
  };

  const q = search.trim().toLowerCase();
  const matches = q
    ? devices
        .filter(d => !(members ?? []).includes(d.serial))
        .filter(
          d =>
            d.serial.toLowerCase().includes(q) ||
            (d.nickname || '').toLowerCase().includes(q) ||
            (d.model || '').toLowerCase().includes(q),
        )
        .slice(0, 8)
    : [];

  const addOne = async (d: DeviceInfo) => {
    setAdding(true);
    try {
      await addDevicesToGroup(group.id, [d.serial]);
      toast.success(`Added ${displayName(d)} to ${group.name}.`);
      setSearch('');
      await onChanged();
    } catch (err) {
      toast.error(errorMessage(err, `add ${displayName(d)}`));
    } finally {
      setAdding(false);
    }
  };

  const addAllConnected = async () => {
    const connected = devices.filter(d => d.state === 'device').map(d => d.serial);
    if (connected.length === 0) {
      toast.info('No connected phones to add.');
      return;
    }
    setAdding(true);
    try {
      await addDevicesToGroup(group.id, connected);
      toast.success(`Added ${connected.length} connected phone${connected.length !== 1 ? 's' : ''} to ${group.name}.`);
      await onChanged();
    } catch (err) {
      toast.error(errorMessage(err, 'add the connected phones'));
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      {isAdmin && (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-end gap-3">
            <Field label="Add a phone" hint="Search by name, model or serial." className="flex min-w-[200px] flex-1 flex-col gap-2">
              <input
                type="search"
                className="vx-input"
                value={search}
                onChange={e => setSearch(e.target.value)}
                autoComplete="off"
              />
            </Field>
            <Button size="lg" onClick={addAllConnected} disabled={adding}>
              Add all connected
            </Button>
          </div>
          {q && (
            <ul aria-label="Phones to add" className="divide-y divide-vx-hairline border border-vx-hairline">
              {matches.length === 0 ? (
                <li className="px-3 py-3 text-sm text-vx-fg-muted">No phone matches “{search.trim()}” outside this group.</li>
              ) : (
                matches.map(d => (
                  <li key={d.serial}>
                    <button
                      type="button"
                      onClick={() => addOne(d)}
                      disabled={adding}
                      className="flex min-h-touch w-full items-center gap-3 px-3 py-2 text-left transition-colors hover:bg-vx-elevated disabled:opacity-50"
                    >
                      <Plus size={14} className="shrink-0 text-vx-fg-muted" aria-hidden />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm text-vx-fg">{displayName(d)}</span>
                        <span className="block truncate font-mono text-[11px] tracking-wide text-vx-fg-label">{d.serial}</span>
                      </span>
                      <StatusChip status={deviceStatus(d)} withHint={false} />
                      <span className="sr-only">Add to {group.name}</span>
                    </button>
                  </li>
                ))
              )}
            </ul>
          )}
        </div>
      )}

      {members === null ? (
        <p className="text-sm text-vx-fg-muted">Loading phones…</p>
      ) : members.length === 0 ? (
        <p className="text-sm text-vx-fg-muted">
          No phones in this group yet.{isAdmin ? ' Search above to add one.' : ''}
        </p>
      ) : (
        <ul className="divide-y divide-vx-hairline border border-vx-hairline">
          {members.map(serial => {
            const d = info(serial);
            return (
              <li key={serial} className="flex min-h-touch items-center gap-3 px-3 py-2">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium text-vx-fg">{nameOf(serial)}</span>
                  <span className="block truncate font-mono text-[11px] tracking-wide text-vx-fg-label">{serial}</span>
                </span>
                {d ? (
                  <StatusChip status={deviceStatus(d)} />
                ) : (
                  <span className="font-mono text-[11px] uppercase tracking-wide text-vx-fg-label">Not visible to you</span>
                )}
                {isAdmin && (
                  <Button
                    variant="quiet"
                    size="sm"
                    iconOnly
                    aria-label={`Remove ${nameOf(serial)} from ${group.name}`}
                    icon={<X size={16} />}
                    onClick={() => setRemoving(serial)}
                  />
                )}
              </li>
            );
          })}
        </ul>
      )}

      <ConfirmDialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={removing ? `Remove ${nameOf(removing)} from ${group.name}?` : ''}
        consequence="The phone stays registered. People who had access to it only through this group lose it."
        confirmLabel="Remove"
        formatError={err => errorMessage(err, 'remove the phone from the group')}
        onConfirm={async () => {
          if (!removing) return;
          const name = nameOf(removing);
          await removeDeviceFromGroup(group.id, removing);
          toast.success(`Removed ${name} from ${group.name}.`);
          await onChanged();
        }}
      />
    </div>
  );
}
