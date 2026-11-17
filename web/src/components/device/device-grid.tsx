'use client';

import { DeviceInfo } from '@/lib/api';
import { DeviceCard } from './device-card';

interface DeviceGridProps {
  devices: DeviceInfo[];
  onEdit?: (serial: string) => void;
  onDelete?: (serial: string) => void;
  onReconnect?: (serial: string) => void;
}

/**
 * Square cells with hairline borders: the grid draws the top and left edge,
 * each card the right and bottom, so a part-filled last row has no stray cells.
 * The page shows its own empty states, so an empty list renders nothing.
 */
export function DeviceGrid({ devices, onEdit, onDelete, onReconnect }: DeviceGridProps) {
  if (devices.length === 0) return null;

  return (
    <div className="grid grid-cols-2 border-l border-t border-vx-hairline md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
      {devices.map((device) => (
        <DeviceCard key={device.serial} device={device} onEdit={onEdit} onDelete={onDelete} onReconnect={onReconnect} />
      ))}
    </div>
  );
}
