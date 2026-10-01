'use client';

import { useEffect, useState } from 'react';
import { discoverDevices, provisionEthernet, DeviceInfo } from '@/lib/api';
import { arrivedOnLAN, networkLabel, networkState } from '@/lib/device-network';

type Phase = 'idle' | 'switching' | 'waiting' | 'done';

/**
 * Moves a phone from USB to its ethernet adapter: switches its adb to the
 * network while it's still on USB, then waits for Batter to find it on the
 * phone network. Also how a phone is brought back after a restart, which
 * turns adb over the network off.
 */
export function EthernetPanel({ serial, device }: { serial: string; device?: DeviceInfo | null }) {
  const [phase, setPhase] = useState<Phase>(device && arrivedOnLAN(device) ? 'done' : 'idle');
  const [address, setAddress] = useState(device?.lan_address ?? '');
  const [error, setError] = useState('');

  useEffect(() => {
    if (phase !== 'waiting') return;
    let cancelled = false;
    const id = setInterval(async () => {
      try {
        const found = (await discoverDevices()).find((d) => d.serial === serial);
        if (!cancelled && found && arrivedOnLAN(found)) {
          setAddress(found.lan_address ?? '');
          setPhase('done');
        }
      } catch {
        // keep waiting
      }
    }, 2000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [phase, serial]);

  const start = async () => {
    setPhase('switching');
    setError('');
    try {
      await provisionEthernet(serial);
      setPhase('waiting');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to switch the phone to the network');
      setPhase('idle');
    }
  };

  if (phase === 'done') {
    return (
      <p className="text-xs text-green-400">
        On the phone network{address ? ` (${address})` : ''}. Batter controls it over ethernet, and it reaches the
        internet only through its VPN profile.
      </p>
    );
  }

  if (phase === 'waiting') {
    return (
      <div className="space-y-2">
        <ol className="list-decimal list-inside text-xs text-gray-300 space-y-1">
          <li>Unplug the USB cable.</li>
          <li>Plug the ethernet adapter (with its power supply) into the phone.</li>
        </ol>
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-xs text-gray-400">Waiting for the phone on the network...</span>
        </div>
        <button onClick={start} className="text-[10px] text-gray-500 hover:text-gray-300">
          Still on USB? Switch again
        </button>
      </div>
    );
  }

  const reprovision = device ? networkState(device) === 'needs-reprovision' : false;
  return (
    <div className="space-y-2">
      <p className="text-xs text-gray-300">
        {reprovision ? (
          <>The phone restarted, which turns adb over the network off. Plug it into USB, then switch it back.</>
        ) : (
          <>
            Move the phone to its ethernet adapter: it then has internet only through its VPN profile, and Batter
            controls it over the phone network. Keep it on USB for this step.
          </>
        )}
      </p>
      {device && <p className="text-[10px] text-gray-500">Now: {networkLabel(device)}</p>}
      <button
        onClick={start}
        disabled={phase === 'switching'}
        className="px-2.5 py-1.5 rounded bg-brand-600 hover:bg-brand-700 text-white text-xs disabled:opacity-50"
      >
        {phase === 'switching' ? 'Switching...' : 'Switch to ethernet'}
      </button>
      {error && <p className="text-[10px] text-red-400">{error}</p>}
    </div>
  );
}
