'use client';

import { Layers, Smartphone, X } from 'lucide-react';
import { Button } from '@/components/ui';
import { DataTable } from './data-table';
import { PermissionChip } from './chips';

export interface Grant {
  id: string;
  device_serial?: string;
  group_id?: string;
  group_name?: string;
  permission: string;
}

/** The name to show for what a grant points at: a phone's name or a group's. */
export function grantTarget(g: Grant, deviceNames: Record<string, string>): string {
  if (g.device_serial) return deviceNames[g.device_serial] || g.device_serial;
  return g.group_name || g.group_id || 'Unknown group';
}

/** Access grants as a hairline table. Revoking is left to the caller, which asks first. */
export function GrantsTable<G extends Grant>({
  grants,
  deviceNames,
  onRevoke,
}: {
  grants: G[];
  deviceNames: Record<string, string>;
  onRevoke: (g: G) => void;
}) {
  return (
    <DataTable
      caption="Access grants"
      rows={grants}
      rowKey={(g) => g.id}
      columns={[
        {
          key: 'target',
          header: 'Access to',
          primary: true,
          cell: (g) => (
            <div className="flex items-center gap-3">
              <span className="text-vx-fg-label" aria-hidden>
                {g.device_serial ? <Smartphone size={16} /> : <Layers size={16} />}
              </span>
              <div className="min-w-0">
                <div className="text-sm text-vx-fg">{grantTarget(g, deviceNames)}</div>
                <div className="vx-label">
                  {g.device_serial ? (
                    <>
                      Phone
                      {deviceNames[g.device_serial] && <span className="normal-case"> · {g.device_serial}</span>}
                    </>
                  ) : (
                    'Group of phones'
                  )}
                </div>
              </div>
            </div>
          ),
        },
        { key: 'permission', header: 'Permission', cell: (g) => <PermissionChip permission={g.permission} /> },
      ]}
      actions={(g) => (
        <Button
          variant="quiet"
          size="md"
          iconOnly
          aria-label={`Revoke access to ${grantTarget(g, deviceNames)}`}
          icon={<X size={16} />}
          className="max-sm:h-touch max-sm:w-touch"
          onClick={() => onRevoke(g)}
        />
      )}
    />
  );
}
