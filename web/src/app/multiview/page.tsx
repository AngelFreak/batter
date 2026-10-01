'use client';

import { useEffect, useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import { listDevices, DeviceInfo } from '@/lib/api';
import { getToken } from '@/lib/auth';
import { MultiViewCell } from '@/components/device/multi-view-cell';

type GridSize = '2x2' | '3x3' | '4x4';

const gridCols: Record<GridSize, string> = {
  '2x2': 'grid-cols-2',
  '3x3': 'grid-cols-3',
  '4x4': 'grid-cols-4',
};

export default function MultiViewPage() {
  const router = useRouter();
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [gridSize, setGridSize] = useState<GridSize>('2x2');
  const [activeSerial, setActiveSerial] = useState<string | null>(null);

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login');
      return;
    }
  }, [router]);

  const fetchDevices = useCallback(async () => {
    try {
      const data = await listDevices();
      setDevices(data);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchDevices();
    const interval = setInterval(fetchDevices, 5000);
    return () => clearInterval(interval);
  }, [fetchDevices]);

  const streamingDevices = devices.filter(d => d.has_session);

  return (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
        <div>
          <h2 className="text-lg font-semibold text-white">Multi View</h2>
          <p className="text-xs text-gray-500">
            {streamingDevices.length} streaming device{streamingDevices.length !== 1 ? 's' : ''}
            {activeSerial && ` — controlling ${activeSerial}`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {(['2x2', '3x3', '4x4'] as GridSize[]).map(size => (
            <button
              key={size}
              onClick={() => setGridSize(size)}
              className={`px-2 py-1 text-[10px] rounded ${
                gridSize === size ? 'bg-brand-600 text-white' : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
              }`}
            >
              {size}
            </button>
          ))}
          <button
            onClick={fetchDevices}
            className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg"
          >
            Refresh
          </button>
        </div>
      </div>

      {/* Grid */}
      <div className="flex-1 p-4 overflow-hidden min-h-0">
        {loading ? (
          <div className="text-gray-500 text-sm">Loading devices...</div>
        ) : streamingDevices.length === 0 ? (
          <div className="text-gray-500 text-sm">No streaming devices. Start sessions from the Devices page.</div>
        ) : (
          <div className={`grid ${gridCols[gridSize]} gap-3 h-full`}>
            {streamingDevices.map(device => (
              <MultiViewCell
                key={device.serial}
                device={device}
                active={activeSerial === device.serial}
                onActivate={() => setActiveSerial(device.serial)}
              />
            ))}
          </div>
        )}
      </div>
    </>
  );
}
