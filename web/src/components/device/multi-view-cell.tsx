'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { Maximize2 } from 'lucide-react';
import { DeviceVideoPlayer } from '@/lib/device-video';
import { DeviceInputHandler } from '@/lib/device-input';
import { DeviceInfo } from '@/lib/api';
import { displayName } from '@/lib/dashboard-filter';
import { streamStatus } from '@/lib/device-status';
import { StatusChip, cx } from '@/components/ui';

interface MultiViewCellProps {
  device: DeviceInfo;
  /** This cell is the one being controlled. */
  active: boolean;
  onActivate: () => void;
  onDeactivate: () => void;
}

/**
 * One phone's live screen. Clicking the screen makes it the controlled cell;
 * the Open link in the header goes to that phone's viewer.
 */
export function MultiViewCell({ device, active, onActivate, onDeactivate }: MultiViewCellProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const playerRef = useRef<DeviceVideoPlayer | null>(null);
  const inputRef = useRef<DeviceInputHandler | null>(null);
  const [videoStatus, setVideoStatus] = useState('connecting');
  const [controlStatus, setControlStatus] = useState('');

  useEffect(() => {
    if (!canvasRef.current) return;

    const player = new DeviceVideoPlayer(canvasRef.current);
    playerRef.current = player;
    player.setOnStatusChange(setVideoStatus);
    player.connect(device.serial);

    return () => {
      player.disconnect();
      playerRef.current = null;
    };
  }, [device.serial]);

  // Connect/disconnect the input handler with the active state. Keys reach the
  // phone only while the canvas has focus, so focus it once connected.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !active) {
      setControlStatus('');
      return;
    }

    const input = new DeviceInputHandler(canvas);
    inputRef.current = input;
    input.setOnStatusChange(setControlStatus);
    input.connect(device.serial);
    canvas.tabIndex = 0;
    canvas.focus();

    return () => {
      input.disconnect();
      inputRef.current = null;
    };
  }, [active, device.serial]);

  const name = displayName(device);
  const denied = active && controlStatus === 'denied';

  return (
    <div
      className={cx(
        'flex min-h-0 flex-col bg-vx-surface',
        active && 'outline outline-2 -outline-offset-2 outline-vx-accent',
      )}
    >
      <div className="flex shrink-0 items-center justify-between gap-2 border-b border-vx-hairline py-1 pl-3 pr-1">
        <span className="min-w-0 truncate text-[13px] font-semibold text-vx-fg">{name}</span>
        <Link
          href={`/devices/${encodeURIComponent(device.serial)}`}
          aria-label={`Open ${name}`}
          title={`Open ${name}`}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-sm text-vx-fg-muted transition-colors hover:text-vx-accent"
        >
          <Maximize2 size={16} aria-hidden />
        </Link>
      </div>
      <div className="flex aspect-[9/19.5] min-h-0 flex-1 items-center justify-center overflow-hidden bg-vx-recessed nav:aspect-auto">
        <canvas
          ref={canvasRef}
          className={cx('max-h-full max-w-full object-contain', !active && 'cursor-pointer')}
          tabIndex={0}
          aria-label={active ? `${name}: controlling. Keys go to this phone; Shift+Escape returns to the page.` : `Control ${name}`}
          onClick={active ? undefined : onActivate}
          onKeyDown={
            active
              ? undefined
              : e => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onActivate();
                  }
                }
          }
        />
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-x-2 gap-y-1 border-t border-vx-hairline px-3 py-2">
        <span className="min-w-0 truncate font-mono text-[11px] tracking-wide text-vx-fg-label">{device.serial}</span>
        <span className="flex items-center gap-3">
          {denied && <StatusChip status={streamStatus('denied')} />}
          {active && !denied && (
            <button
              type="button"
              onClick={onDeactivate}
              title="Stop controlling this phone"
              className="whitespace-nowrap font-mono text-[11px] uppercase tracking-wide text-vx-accent hover:underline"
            >
              Controlling · stop
            </button>
          )}
          <StatusChip status={streamStatus(videoStatus)} />
        </span>
      </div>
    </div>
  );
}
