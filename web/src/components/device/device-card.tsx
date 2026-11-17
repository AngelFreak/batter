'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { MoreVertical, Pencil, ShieldCheck, Trash2 } from 'lucide-react';
import { DeviceThumbnailPlayer } from '@/lib/device-video-thumbnail';
import { DeviceInfo, fetchScreenshot } from '@/lib/api';
import { networkLabel, networkState } from '@/lib/device-network';
import { deviceStatus, streamStatus } from '@/lib/device-status';
import { displayName } from '@/lib/dashboard-filter';
import { Button, Menu, StatusChip, StatusGlyph, cx } from '@/components/ui';

interface DeviceCardProps {
  device: DeviceInfo;
  onEdit?: (serial: string) => void;
  onDelete?: (serial: string) => void;
  onReconnect?: (serial: string) => void;
}

export function DeviceCard({ device, onEdit, onDelete, onReconnect }: DeviceCardProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const playerRef = useRef<DeviceThumbnailPlayer | null>(null);
  const [streamCode, setStreamCode] = useState<string>('idle');
  const [screenshotUrl, setScreenshotUrl] = useState<string | null>(null);

  // Always fetch cached screenshot — used as fallback behind live canvas too
  useEffect(() => {
    let cancelled = false;
    fetchScreenshot(device.serial).then((blob) => {
      if (cancelled) return;
      if (blob) {
        setScreenshotUrl((prev) => {
          if (prev) URL.revokeObjectURL(prev);
          return URL.createObjectURL(blob);
        });
      }
    });

    return () => {
      cancelled = true;
      setScreenshotUrl((prev) => {
        if (prev) URL.revokeObjectURL(prev);
        return null;
      });
    };
  }, [device.serial]);

  useEffect(() => {
    if (!device.has_session || !canvasRef.current) return;

    const player = new DeviceThumbnailPlayer(canvasRef.current);
    playerRef.current = player;

    player.setOnStatusChange(setStreamCode);
    player.connect(device.serial);

    return () => {
      player.disconnect();
      playerRef.current = null;
    };
  }, [device.serial, device.has_session]);

  const status = deviceStatus(device);
  const isDisconnected = device.status !== 'connected';
  const name = displayName(device);
  const href = `/devices/${encodeURIComponent(device.serial)}`;
  const net = networkState(device);
  // The status chip already says "offline" and "needs USB"; the network label
  // adds something only when the phone is attached.
  const showNetwork = net === 'usb' || net === 'ethernet';

  // Only a failing stream is worth words on the thumbnail.
  const stream = device.has_session ? streamStatus(streamCode) : null;
  const streamError = stream && stream.tone === 'danger' ? stream : null;

  const dim = isDisconnected ? 'opacity-40 grayscale' : '';

  const items = [
    ...(onEdit ? [{ label: 'Edit', icon: <Pencil />, onSelect: () => onEdit(device.serial) }] : []),
    ...(onDelete ? [{ label: 'Delete', icon: <Trash2 />, danger: true, onSelect: () => onDelete(device.serial) }] : []),
  ];

  return (
    <article className="relative flex min-w-0 flex-col border-b border-r border-vx-hairline bg-vx-surface transition-colors hover:bg-vx-elevated">
      {/* Thumbnail area */}
      <div className="relative aspect-[9/15.5] overflow-hidden bg-vx-recessed">
        {/* Cached screenshot, the base layer when there is one */}
        {screenshotUrl ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={screenshotUrl}
            className={cx('absolute inset-0 h-full w-full object-contain', dim)}
            alt=""
          />
        ) : (
          !device.has_session && (
            <div className="absolute inset-0 flex items-center justify-center">
              <span className="vx-label">No preview</span>
            </div>
          )
        )}

        {/* Live canvas, over the cached screenshot while streaming */}
        {device.has_session && (
          <canvas ref={canvasRef} className={cx('absolute inset-0 h-full w-full object-contain', dim)} />
        )}

        {streamError && (
          <div
            role="status"
            className={cx(
              'absolute inset-x-2 bottom-2 flex items-center gap-1.5 border border-vx-danger/50 bg-vx-recessed/90 px-2 py-1.5',
              'font-mono text-[11px] uppercase leading-tight tracking-wide text-vx-danger',
            )}
          >
            <StatusGlyph icon={streamError.icon} />
            <span className="min-w-0">{streamError.label}</span>
          </div>
        )}
      </div>

      {/* Info */}
      <div className="flex min-w-0 flex-col gap-1.5 px-3 pb-3 pt-2.5">
        <div className="flex items-center justify-between gap-1">
          <Link
            href={href}
            className={cx(
              'min-w-0 flex-1 truncate text-[15px] font-bold leading-snug text-vx-fg',
              // The link's ::after covers the whole card, so the card is the target.
              'after:absolute after:inset-0 focus-visible:outline-none',
              'focus-visible:after:outline focus-visible:after:outline-2 focus-visible:after:-outline-offset-2 focus-visible:after:outline-vx-accent',
            )}
          >
            {name}
            <span className="sr-only">, {status.label}</span>
          </Link>
          {items.length > 0 && (
            <div className="relative z-10 -mr-1.5 shrink-0">
              <Menu
                trigger={(p) => (
                  <Button
                    {...p}
                    variant="quiet"
                    size="sm"
                    iconOnly
                    aria-label={`Actions for ${name}`}
                    icon={<MoreVertical size={16} />}
                  />
                )}
                items={items}
              />
            </div>
          )}
        </div>

        <div className="truncate font-mono text-[12px] leading-tight text-vx-fg-label" title={device.serial}>
          {device.serial}
        </div>

        <StatusChip status={status} withHint={false} />

        {(showNetwork || device.vpn_profile_name) && (
          <div className="flex min-w-0 flex-col gap-0.5 text-[13px] leading-snug text-vx-fg-muted">
            {showNetwork && <span className="truncate">{networkLabel(device)}</span>}
            {device.vpn_profile_name && (
              <span className="flex min-w-0 items-center gap-1.5" title={`VPN: ${device.vpn_profile_name}`}>
                <ShieldCheck size={13} aria-hidden className="shrink-0" />
                <span className="sr-only">VPN: </span>
                <span className="truncate">{device.vpn_profile_name}</span>
              </span>
            )}
          </div>
        )}

        {device.needs_reprovision && onReconnect && (
          <div className="relative z-10 mt-1">
            <Button size="sm" onClick={() => onReconnect(device.serial)}>
              Reconnect
            </Button>
          </div>
        )}
      </div>
    </article>
  );
}
