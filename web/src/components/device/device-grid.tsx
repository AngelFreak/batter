'use client';

import { DeviceInfo } from '@/lib/api';
import { DeviceCard } from './device-card';

interface DeviceGridProps {
  devices: DeviceInfo[];
  onDeviceClick: (serial: string) => void;
  onEdit?: (serial: string) => void;
  onDelete?: (serial: string) => void;
  onReconnect?: (serial: string) => void;
}

export function DeviceGrid({ devices, onDeviceClick, onEdit, onDelete, onReconnect }: DeviceGridProps) {
  if (devices.length === 0) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-center">
          <div className="text-gray-500 text-sm">No devices registered</div>
          <div className="text-gray-600 text-xs mt-1">
            Click &apos;Add Device&apos; to register your first phone.
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4">
      {devices.map((device) => (
        <DeviceCard
          key={device.serial}
          device={device}
          onClick={() => onDeviceClick(device.serial)}
          onEdit={onEdit}
          onDelete={onDelete}
          onReconnect={onReconnect}
        />
      ))}
    </div>
  );
}
