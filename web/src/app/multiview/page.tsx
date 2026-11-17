'use client';

import { useEffect, useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import { LayoutGrid, RefreshCw, Square } from 'lucide-react';
import { listDevices, DeviceInfo } from '@/lib/api';
import { displayName } from '@/lib/dashboard-filter';
import { getToken } from '@/lib/auth';
import { MultiViewCell } from '@/components/device/multi-view-cell';
import { LinkButton } from '@/components/wall/link-button';
import { Banner, Button, EmptyState, PageHeader } from '@/components/ui';

type GridSize = '2x2' | '3x3' | '4x4';

// Two columns on a phone whatever the size; the chosen size applies from `sm`.
const gridCols: Record<GridSize, string> = {
  '2x2': 'grid-cols-2',
  '3x3': 'grid-cols-2 sm:grid-cols-3',
  '4x4': 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-4',
};

export default function MultiViewPage() {
  const router = useRouter();
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [connectionLost, setConnectionLost] = useState(false);
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
      setConnectionLost(false);
    } catch {
      // Keep the last good list on screen and say so.
      setConnectionLost(true);
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
  const n = streamingDevices.length;
  const activeDevice = streamingDevices.find(d => d.serial === activeSerial);

  return (
    <div className="flex flex-col nav:h-full nav:min-h-[480px]">
      <PageHeader
        title="Multi View"
        sub={
          <>
            <span>Watch many phones at once. Click one to control it.</span>
            <span className="font-mono text-[12px] uppercase tracking-wide text-vx-fg-label">
              {n} streaming
            </span>
            {activeDevice && (
              <span className="font-mono text-[12px] uppercase tracking-wide text-vx-accent">
                Controlling {displayName(activeDevice)}
              </span>
            )}
          </>
        }
        actions={
          <>
            {activeDevice && (
              <Button size="sm" icon={<Square size={12} />} onClick={() => setActiveSerial(null)}>
                Stop controlling
              </Button>
            )}
            <div role="group" aria-label="Grid size" className="flex gap-1">
              {(['2x2', '3x3', '4x4'] as GridSize[]).map(size => (
                <Button
                  key={size}
                  size="sm"
                  aria-pressed={gridSize === size}
                  onClick={() => setGridSize(size)}
                >
                  {size}
                </Button>
              ))}
            </div>
            <Button size="sm" icon={<RefreshCw size={14} />} onClick={fetchDevices}>
              Refresh
            </Button>
          </>
        }
      />

      {connectionLost && (
        <Banner tone="danger" className="m-4 mb-0 sm:mx-6">
          Connection lost, retrying. Showing the last phones we saw.
        </Banner>
      )}

      <div className="flex flex-col p-4 sm:p-6 nav:min-h-0 nav:flex-1">
        {loading ? (
          <p className="text-sm text-vx-fg-muted">Loading phones…</p>
        ) : devices.length === 0 ? (
          <EmptyState icon={<LayoutGrid />} title="No phones yet" action={<LinkButton href="/dashboard">Go to Devices</LinkButton>}>
            Nothing is registered, or you don&rsquo;t have access to any phone. Ask an admin, or register a phone on the Devices page.
          </EmptyState>
        ) : n === 0 ? (
          <EmptyState icon={<LayoutGrid />} title="No sessions running" action={<LinkButton href="/dashboard">Go to Devices</LinkButton>}>
            Multi View shows phones that have a session. Start one on the Devices page, then come back.
          </EmptyState>
        ) : (
          <div
            className={`grid gap-px border nav:min-h-0 nav:flex-1 nav:auto-rows-[minmax(240px,1fr)] nav:overflow-auto border-vx-hairline bg-vx-hairline ${gridCols[gridSize]}`}
          >
            {streamingDevices.map(device => (
              <MultiViewCell
                key={device.serial}
                device={device}
                active={activeSerial === device.serial}
                onActivate={() => setActiveSerial(device.serial)}
                onDeactivate={() => setActiveSerial(null)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
