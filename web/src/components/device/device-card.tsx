'use client';

import { useEffect, useRef, useState } from 'react';
import { DeviceThumbnailPlayer } from '@/lib/device-video-thumbnail';
import { DeviceInfo, fetchScreenshot } from '@/lib/api';
import { networkState } from '@/lib/device-network';

interface DeviceCardProps {
  device: DeviceInfo;
  onClick: () => void;
  onEdit?: (serial: string) => void;
  onDelete?: (serial: string) => void;
}

const STATUS_COLORS: Record<string, string> = {
  connected: 'bg-green-500',
  disconnected: 'bg-gray-500',
  offline: 'bg-yellow-500',
  unauthorized: 'bg-red-500',
};

export function DeviceCard({ device, onClick, onEdit, onDelete }: DeviceCardProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const playerRef = useRef<DeviceThumbnailPlayer | null>(null);
  const [status, setStatus] = useState<string>('idle');
  const [showMenu, setShowMenu] = useState(false);
  const [screenshotUrl, setScreenshotUrl] = useState<string | null>(null);

  // Always fetch cached screenshot — used as fallback behind live canvas too
  useEffect(() => {
    let cancelled = false;
    fetchScreenshot(device.serial).then((blob) => {
      if (cancelled) return;
      if (blob) {
        setScreenshotUrl((prev) => { if (prev) URL.revokeObjectURL(prev); return URL.createObjectURL(blob); });
      }
    });

    return () => {
      cancelled = true;
      setScreenshotUrl((prev) => { if (prev) URL.revokeObjectURL(prev); return null; });
    };
  }, [device.serial]);

  useEffect(() => {
    if (!device.has_session || !canvasRef.current) return;

    const player = new DeviceThumbnailPlayer(canvasRef.current);
    playerRef.current = player;

    player.setOnStatusChange(setStatus);
    player.connect(device.serial);

    return () => {
      player.disconnect();
      playerRef.current = null;
    };
  }, [device.serial, device.has_session]);

  const statusColor = STATUS_COLORS[device.status] || 'bg-gray-500';
  const isDisconnected = device.status !== 'connected';
  const label = device.nickname || device.model || device.serial;

  return (
    <div
      onClick={onClick}
      className="group cursor-pointer rounded-lg bg-gray-900 border border-gray-800 overflow-hidden hover:border-brand-500/50 transition-colors relative"
    >
      {/* Kebab menu */}
      {(onEdit || onDelete) && (
        <div
          className="absolute top-1.5 right-1.5 z-10 opacity-0 group-hover:opacity-100 transition-opacity"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            onClick={() => setShowMenu(!showMenu)}
            className="w-6 h-6 flex items-center justify-center rounded bg-gray-800/80 hover:bg-gray-700 text-gray-400 hover:text-gray-200 text-xs"
          >
            &#8942;
          </button>
          {showMenu && (
            <div className="absolute right-0 mt-1 bg-gray-800 border border-gray-700 rounded-lg shadow-xl py-1 min-w-[100px]">
              {onEdit && (
                <button
                  onClick={() => {
                    setShowMenu(false);
                    onEdit(device.serial);
                  }}
                  className="w-full text-left px-3 py-1.5 text-xs text-gray-300 hover:bg-gray-700"
                >
                  Edit
                </button>
              )}
              {onDelete && (
                <button
                  onClick={() => {
                    setShowMenu(false);
                    if (confirm(`Delete device ${device.serial}?`)) {
                      onDelete(device.serial);
                    }
                  }}
                  className="w-full text-left px-3 py-1.5 text-xs text-red-400 hover:bg-gray-700"
                >
                  Delete
                </button>
              )}
            </div>
          )}
        </div>
      )}

      {/* Thumbnail area */}
      <div className="relative aspect-[9/16] bg-gray-950 flex items-center justify-center overflow-hidden">
        {/* Cached screenshot — always shown as base layer when available */}
        {screenshotUrl ? (
          <img
            src={screenshotUrl}
            className={`absolute inset-0 w-full h-full object-contain ${isDisconnected ? 'brightness-50' : ''}`}
            alt=""
          />
        ) : !device.has_session && (
          <div className="text-gray-600 text-xs">
            {isDisconnected ? 'Disconnected' : 'No session'}
          </div>
        )}

        {/* Live canvas — overlays cached screenshot when streaming */}
        {device.has_session && (
          <canvas
            ref={canvasRef}
            className={`absolute inset-0 w-full h-full object-contain ${isDisconnected ? 'brightness-50' : ''}`}
          />
        )}

        {/* Darken overlay for disconnected devices without screenshot */}
        {isDisconnected && !screenshotUrl && !device.has_session && (
          <div className="absolute inset-0 bg-gray-950/40" />
        )}

        {/* Connection status badge */}
        <div className={`absolute top-2 left-2 z-10 flex items-center gap-1.5 px-2 py-1 rounded text-[10px] font-medium ${
          isDisconnected
            ? 'bg-gray-900/80 text-gray-400'
            : 'bg-gray-900/80 text-green-400'
        }`}>
          <div className={`w-1.5 h-1.5 rounded-full ${isDisconnected ? 'bg-gray-500' : 'bg-green-500'}`} />
          {isDisconnected ? 'Disconnected' : 'Connected'}
        </div>

        {/* Status overlay */}
        {device.has_session && status !== 'streaming' && status !== 'idle' && (
          <div className="absolute inset-0 bg-gray-950/80 flex items-center justify-center">
            <span className="text-gray-400 text-xs">{status}</span>
          </div>
        )}
      </div>

      {/* Info bar */}
      <div className="px-3 py-2 border-t border-gray-800">
        <div className="flex items-center gap-2">
          <div className={`w-2 h-2 rounded-full flex-shrink-0 ${statusColor}`} />
          <span className="text-xs font-medium text-gray-200 truncate">
            {label}
          </span>
        </div>
        <div className="text-[10px] text-gray-500 mt-0.5 truncate">
          {device.serial}
          {networkState(device) === 'ethernet' && ' · Ethernet'}
        </div>
        {device.needs_reprovision && (
          <div className="text-[10px] text-yellow-400 mt-0.5 truncate" title="The phone restarted, which turns adb over the network off. Plug it into USB and switch it back in Edit device.">
            Needs USB re-provision
          </div>
        )}
      </div>
    </div>
  );
}
