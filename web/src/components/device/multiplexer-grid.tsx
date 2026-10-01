'use client';

import { useEffect, useRef, useState } from 'react';
import { DeviceVideoPlayer } from '@/lib/device-video';
import { DeviceInfo } from '@/lib/api';

interface MultiplexerTileProps {
  device: DeviceInfo;
  isPrimary: boolean;
  canvasRef?: (el: HTMLCanvasElement | null) => void;
}

function MultiplexerTile({ device, isPrimary, canvasRef }: MultiplexerTileProps) {
  const localCanvasRef = useRef<HTMLCanvasElement>(null);
  const playerRef = useRef<DeviceVideoPlayer | null>(null);
  const [videoStatus, setVideoStatus] = useState('connecting');

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

  const statusDot = videoStatus === 'streaming' ? 'bg-green-500' : 'bg-yellow-500';

  return (
    <div className={`flex flex-col bg-gray-900 rounded-lg overflow-hidden border-2 min-h-0 ${
      isPrimary ? 'border-brand-500' : 'border-gray-800'
    }`}>
      <div className="flex-1 min-h-0 bg-black flex items-center justify-center overflow-hidden" style={{ aspectRatio: '9 / 19.5' }}>
        <canvas
          ref={localCanvasRef}
          className="max-w-full max-h-full object-contain"
          tabIndex={isPrimary ? 0 : undefined}
          style={{ cursor: isPrimary ? 'default' : 'not-allowed' }}
        />
      </div>
      <div className="flex items-center gap-2 px-2 py-1.5 bg-gray-900 border-t border-gray-800 shrink-0">
        <span className={`w-1.5 h-1.5 rounded-full ${statusDot}`} />
        <span className="text-[10px] text-gray-300 truncate">
          {device.nickname || device.model || device.serial}
        </span>
        {isPrimary && (
          <span className="text-[10px] text-brand-400 ml-auto">INPUT</span>
        )}
      </div>
    </div>
  );
}

interface MultiplexerGridProps {
  devices: DeviceInfo[];
  primaryCanvasRef: (el: HTMLCanvasElement | null) => void;
}

export function MultiplexerGrid({ devices, primaryCanvasRef }: MultiplexerGridProps) {
  const cols = devices.length <= 2 ? 'grid-cols-2' :
               devices.length <= 4 ? 'grid-cols-2' :
               devices.length <= 9 ? 'grid-cols-3' : 'grid-cols-4';

  return (
    <div className={`grid ${cols} gap-3 h-full`}>
      {devices.map((device, index) => (
        <MultiplexerTile
          key={device.serial}
          device={device}
          isPrimary={index === 0}
          canvasRef={index === 0 ? primaryCanvasRef : undefined}
        />
      ))}
    </div>
  );
}
