'use client';

import { useEffect, useRef, useState } from 'react';
import { discoverDevices, getDevice, provisionEthernet, DeviceInfo } from '@/lib/api';
import { DeviceNetwork, networkState, reconnectStep } from '@/lib/device-network';

type StepState = 'done' | 'active' | 'waiting' | 'pending';

// The phone as Batter sees it now. A phone added in the wizard may not be
// registered yet, so fall back to adb's own list.
async function fetchPhone(serial: string): Promise<DeviceNetwork | null> {
  try {
    return await getDevice(serial);
  } catch {
    return (await discoverDevices()).find((d) => d.serial === serial) ?? null;
  }
}

function Step({ n, state, title, children }: { n: number; state: StepState; title: string; children?: React.ReactNode }) {
  const marker =
    state === 'done' ? (
      <span className="flex h-5 w-5 items-center justify-center rounded-full bg-green-600 text-[10px] text-white">✓</span>
    ) : state === 'waiting' ? (
      <span className="block h-5 w-5 rounded-full border-2 border-brand-500 border-t-transparent animate-spin" />
    ) : (
      <span
        className={`flex h-5 w-5 items-center justify-center rounded-full border text-[10px] ${
          state === 'active' ? 'border-brand-500 text-brand-400' : 'border-gray-700 text-gray-600'
        }`}
      >
        {n}
      </span>
    );
  return (
    <li className="flex gap-2.5">
      <div className="flex-shrink-0 pt-px">{marker}</div>
      <div className="min-w-0 flex-1">
        <div className={`text-xs ${state === 'pending' ? 'text-gray-500' : 'text-gray-200'}`}>{title}</div>
        {children && <div className="mt-1 space-y-1.5">{children}</div>}
      </div>
    </li>
  );
}

/**
 * Moves a phone from USB to its ethernet adapter, or brings it back after a
 * restart (which turns adb over the network off; only a computer on USB can
 * turn it on again). Each step shows what Batter sees right now: the phone
 * on USB, then adb over the network switched on, then the phone back on the
 * phone network.
 */
export function EthernetPanel({
  serial,
  device,
  onDone,
}: {
  serial: string;
  device?: DeviceInfo | null;
  onDone?: () => void;
}) {
  const [phone, setPhone] = useState<DeviceNetwork | null>(device ?? null);
  const [restarted] = useState(device ? networkState(device) === 'needs-reprovision' : false);
  const [switched, setSwitched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const step = phone ? reconnectStep(phone) : 'plug-usb';
  const done = step === 'done';

  useEffect(() => {
    if (done) return;
    let cancelled = false;
    const poll = async () => {
      try {
        const p = await fetchPhone(serial);
        if (!cancelled && p) setPhone(p);
      } catch {
        // keep polling
      }
    };
    poll();
    const id = setInterval(poll, 2000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [serial, done]);

  const notified = useRef(false);
  useEffect(() => {
    if (done && !notified.current) {
      notified.current = true;
      onDone?.();
    }
  }, [done, onDone]);

  const switchToEthernet = async () => {
    setBusy(true);
    setError('');
    try {
      await provisionEthernet(serial);
      setSwitched(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to switch the phone to the network');
    } finally {
      setBusy(false);
    }
  };

  if (done) {
    return (
      <p className="text-xs text-green-400">
        On the phone network{phone?.lan_address ? ` (${phone.lan_address})` : ''}. Batter controls it over ethernet,
        and it reaches the internet only through its VPN profile.
      </p>
    );
  }

  const onUSB = step === 'switch';
  const usbState: StepState = switched || onUSB ? 'done' : step === 'allow-usb' ? 'active' : 'waiting';
  const switchState: StepState = switched ? 'done' : onUSB ? 'active' : 'pending';
  const adapterState: StepState = switched ? 'waiting' : 'pending';

  return (
    <div className="space-y-3">
      <p className="text-xs text-gray-300">
        {restarted ? (
          <>
            The phone restarted, which turns adb over the network off, so Batter can&apos;t control it. Android only
            lets a computer on USB turn it back on.
          </>
        ) : (
          <>
            Move the phone to its ethernet adapter: it then has internet only through its VPN profile, and Batter
            controls it over the phone network.
          </>
        )}
      </p>
      <ol className="space-y-3">
        <Step n={1} state={usbState} title="Plug the phone into Batter's USB">
          {usbState === 'waiting' && (
            <p className="text-[10px] text-gray-500">
              {restarted ? 'Take the ethernet adapter out first: the phone has one port. ' : ''}
              Waiting for the phone on USB...
            </p>
          )}
          {step === 'allow-usb' && (
            <p className="text-[10px] text-yellow-400">
              On the phone, tap <b>Allow</b> on &quot;Allow USB debugging?&quot; and tick &quot;Always allow from this
              computer&quot;.
            </p>
          )}
        </Step>
        <Step n={2} state={switchState} title="Switch on adb over the network">
          {switchState === 'active' && (
            <button
              onClick={switchToEthernet}
              disabled={busy}
              className="px-2.5 py-1.5 rounded bg-brand-600 hover:bg-brand-700 text-white text-xs disabled:opacity-50"
            >
              {busy ? 'Switching...' : 'Switch to ethernet'}
            </button>
          )}
          {error && <p className="text-[10px] text-red-400">{error}</p>}
        </Step>
        <Step n={3} state={adapterState} title="Unplug USB and plug the ethernet adapter (with its charger) back in">
          {switched && (
            <>
              <p className="text-[10px] text-gray-500">Waiting for the phone on the phone network...</p>
              {onUSB && (
                <button onClick={switchToEthernet} disabled={busy} className="text-[10px] text-gray-500 hover:text-gray-300">
                  Still on USB after a while? Switch again
                </button>
              )}
            </>
          )}
        </Step>
      </ol>
    </div>
  );
}
