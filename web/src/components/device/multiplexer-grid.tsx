'use client';

import { useEffect, useRef, useState } from 'react';
import { DeviceVideoPlayer } from '@/lib/device-video';
import { DeviceInfo } from '@/lib/api';
import { displayName } from '@/lib/dashboard-filter';
import { streamStatus } from '@/lib/device-status';
import { StatusChip, cx } from '@/components/ui';

interface MultiplexerTileProps {
  device: DeviceInfo;
  isPrimary: boolean;
  /** The control status the input handler reports for this phone, if any. */
  controlStatus?: string;
  canvasRef?: (el: HTMLCanvasElement | null) => void;
}

function MultiplexerTile({ device, isPrimary, controlStatus, canvasRef }: MultiplexerTileProps) {
  const localCanvasRef = useRef<HTMLCanvasElement>(null);
  const playerRef = useRef<DeviceVideoPlayer | null>(null);
  const [videoStatus, setVideoStatus] = useState('connecting');
  const [focused, setFocused] = useState(false);

  useEffect(() => {
    const canvas = localCanvasRef.current;
    if (!canvas) return;

    if (canvasRef) canvasRef(canvas);

    const player = new DeviceVideoPlayer(canvas);
    playerRef.current = player;
    player.setOnStatusChange(setVideoStatus);
    player.connect(device.serial);

    return () => {
      player.disconnect();
      playerRef.current = null;
      if (canvasRef) canvasRef(null);
    };
  }, [device.serial, canvasRef]);

  const name = displayName(device);
  // Control problems are worth a word on the tile; "connected" is the normal case.
  const controlProblem = controlStatus === 'denied' || controlStatus === 'disconnected';

  return (
    <div
      className={cx(
        'flex min-h-0 flex-col bg-vx-surface',
        isPrimary && 'outline outline-2 -outline-offset-2 outline-vx-accent',
      )}
    >
      <div className="flex aspect-[9/19.5] min-h-0 flex-1 items-center justify-center overflow-hidden bg-vx-recessed nav:aspect-auto">
        <canvas
          ref={localCanvasRef}
          className={cx('max-h-full max-w-full object-contain', isPrimary && 'focus-visible:outline-offset-[-2px]')}
          tabIndex={isPrimary ? 0 : undefined}
          aria-label={isPrimary ? `${name}: input screen. Keys and clicks go to every phone shown; Shift+Escape returns to the page.` : undefined}
          aria-hidden={isPrimary ? undefined : true}
          onFocus={isPrimary ? () => setFocused(true) : undefined}
          onBlur={isPrimary ? () => setFocused(false) : undefined}
        />
      </div>
      <div className="flex shrink-0 flex-col gap-1 border-t border-vx-hairline px-3 py-2">
        <div className="flex items-center justify-between gap-2">
          <span className="min-w-0 truncate text-[13px] font-semibold text-vx-fg">{name}</span>
          <StatusChip status={streamStatus(videoStatus)} />
        </div>
        <div className="flex items-center justify-between gap-2">
          <span className="min-w-0 truncate font-mono text-[11px] tracking-wide text-vx-fg-label">{device.serial}</span>
          {controlProblem ? (
            <StatusChip status={streamStatus(controlStatus!)} />
          ) : isPrimary ? (
            <span className="whitespace-nowrap font-mono text-[11px] uppercase tracking-wide text-vx-accent">
              {focused ? 'Keys go to all' : 'Input · click to type'}
            </span>
          ) : (
            <span className="whitespace-nowrap font-mono text-[11px] uppercase tracking-wide text-vx-fg-muted">
              Mirroring
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

interface MultiplexerGridProps {
  devices: DeviceInfo[];
  primaryCanvasRef: (el: HTMLCanvasElement | null) => void;
  /** Control status by serial, as reported by the input handler. */
  controlStatus?: Record<string, string>;
}

export function MultiplexerGrid({ devices, primaryCanvasRef, controlStatus }: MultiplexerGridProps) {
  const cols =
    devices.length <= 1
      ? 'grid-cols-1'
      : devices.length <= 4
        ? 'grid-cols-2'
        : devices.length <= 9
          ? 'grid-cols-2 sm:grid-cols-3'
          : 'grid-cols-2 sm:grid-cols-3 lg:grid-cols-4';

  return (
    <div
      className={`grid gap-px border border-vx-hairline bg-vx-hairline nav:h-full nav:auto-rows-[minmax(240px,1fr)] nav:overflow-auto ${cols}`}
    >
      {devices.map((device, index) => (
        <MultiplexerTile
          key={device.serial}
          device={device}
          isPrimary={index === 0}
          controlStatus={controlStatus?.[device.serial]}
          canvasRef={index === 0 ? primaryCanvasRef : undefined}
        />
      ))}
    </div>
  );
}
