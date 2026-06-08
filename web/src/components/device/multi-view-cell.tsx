'use client';

import { useEffect, useRef, useState } from 'react';
import { DeviceVideoPlayer } from '@/lib/device-video';
import { DeviceInputHandler } from '@/lib/device-input';
import { DeviceInfo } from '@/lib/api';

interface MultiViewCellProps {
  device: DeviceInfo;
  active: boolean;
  onActivate: () => void;
}

export function MultiViewCell({ device, active, onActivate }: MultiViewCellProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const playerRef = useRef<DeviceVideoPlayer | null>(null);
  const inputRef = useRef<DeviceInputHandler | null>(null);
  const [videoStatus, setVideoStatus] = useState('connecting');

  useEffect(() => {
    if (!canvasRef.current) return;

    const canvas = canvasRef.current;

    const player = new DeviceVideoPlayer(canvas);
    playerRef.current = player;
    player.setOnStatusChange(setVideoStatus);
    player.connect(device.serial);

    return () => {
      player.disconnect();
      playerRef.current = null;
    };
  }, [device.serial]);

  // Connect/disconnect input handler based on active state
  useEffect(() => {
    if (!canvasRef.current) return;

    if (active) {
      const canvas = canvasRef.current;
      const input = new DeviceInputHandler(canvas);
      inputRef.current = input;
      input.connect(device.serial);
      canvas.tabIndex = 0;
      canvas.focus();

      return () => {
        input.disconnect();
        inputRef.current = null;
      };
    } else {
      if (inputRef.current) {
        inputRef.current.disconnect();
        inputRef.current = null;
      }
    }
  }, [active, device.serial]);

  const statusDot = videoStatus === 'streaming' ? 'bg-green-500' : 'bg-yellow-500';

  return (
    <div
      className={`flex flex-col bg-gray-900 rounded-lg overflow-hidden border-2 cursor-pointer transition-colors min-h-0 ${
        active ? 'border-brand-500' : 'border-gray-800 hover:border-gray-700'
      }`}
      onClick={onActivate}
    >
      <div className="flex-1 min-h-0 bg-black flex items-center justify-center overflow-hidden" style={{ aspectRatio: '9 / 19.5' }}>
        <canvas
          ref={canvasRef}
          className="max-w-full max-h-full object-contain"
          style={{ cursor: active ? 'default' : 'pointer' }}
        />
      </div>
      <div className="flex items-center gap-2 px-2 py-1.5 bg-gray-900 border-t border-gray-800 shrink-0">
        <span className={`w-1.5 h-1.5 rounded-full ${statusDot}`} />
        <span className="text-[10px] text-gray-300 truncate">
          {device.nickname || device.model || device.serial}
        </span>
        <span className="text-[10px] text-gray-600 truncate ml-auto">
          {device.serial}
        </span>
      </div>
    </div>
  );
}
