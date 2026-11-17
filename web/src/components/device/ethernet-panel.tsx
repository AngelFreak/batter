'use client';

import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Check, LoaderCircle } from 'lucide-react';
import { discoverDevices, getDevice, provisionEthernet, DeviceInfo } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { displayName } from '@/lib/dashboard-filter';
import { DeviceNetwork, networkState, reconnectStep } from '@/lib/device-network';
import { useUser } from '@/lib/use-user';
import { Button, ConfirmDialog, cx, useToast } from '@/components/ui';

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

function Step({ n, state, title, children }: { n: number; state: StepState; title: string; children?: ReactNode }) {
  const marker =
    state === 'done' ? (
      <span className="flex h-5 w-5 items-center justify-center rounded-sm bg-vx-accent text-vx-accent-ink">
        <Check size={14} strokeWidth={3} aria-hidden />
        <span className="sr-only">Done</span>
      </span>
    ) : state === 'waiting' ? (
      <span className="flex h-5 w-5 items-center justify-center text-vx-accent">
        <LoaderCircle size={16} className="animate-spin" aria-hidden />
        <span className="sr-only">Waiting</span>
      </span>
    ) : (
      <span
        className={cx(
          'flex h-5 w-5 items-center justify-center rounded-sm border font-mono text-[11px]',
          state === 'active' ? 'border-vx-accent text-vx-accent' : 'border-vx-control text-vx-fg-label',
        )}
      >
        {n}
      </span>
    );
  return (
    <li className="flex gap-3">
      <div className="shrink-0 pt-px">{marker}</div>
      <div className="min-w-0 flex-1">
        <div className={cx('text-sm', state === 'pending' ? 'text-vx-fg-label' : 'text-vx-fg')}>{title}</div>
        {children && <div className="mt-1.5 space-y-2">{children}</div>}
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
  const toast = useToast();
  const { isOperator, ready } = useUser();
  const [phone, setPhone] = useState<DeviceNetwork | null>(device ?? null);
  const [restarted] = useState(device ? networkState(device) === 'needs-reprovision' : false);
  const [switched, setSwitched] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const step = phone ? reconnectStep(phone) : 'plug-usb';
  const done = step === 'done';
  const name = device ? displayName(device) : serial;
  // The API rejects this for viewers, so don't offer it.
  const canSwitch = ready && isOperator;

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

  // Throws on failure, so the confirmation stays open and shows why.
  const switchToEthernet = async () => {
    await provisionEthernet(serial);
    setSwitched(true);
    toast.success('adb over the network is on. Unplug USB and plug in the ethernet adapter.');
  };

  if (done) {
    return (
      <p className="flex items-start gap-2 text-sm text-vx-fg">
        <Check size={16} className="mt-0.5 shrink-0 text-vx-accent" aria-hidden />
        <span>
          On the phone network{phone?.lan_address ? ` (${phone.lan_address})` : ''}. Batter controls it over ethernet,
          and it reaches the internet only through its VPN profile.
        </span>
      </p>
    );
  }

  const onUSB = step === 'switch';
  const usbState: StepState = switched || onUSB ? 'done' : step === 'allow-usb' ? 'active' : 'waiting';
  const switchState: StepState = switched ? 'done' : onUSB ? 'active' : 'pending';
  const adapterState: StepState = switched ? 'waiting' : 'pending';

  return (
    <div className="space-y-4">
      <p className="text-sm text-vx-fg-muted">
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
      <ol className="space-y-4">
        <Step n={1} state={usbState} title="Plug the phone into Batter's USB">
          {usbState === 'waiting' && (
            <p className="text-[13px] text-vx-fg-muted">
              {restarted ? 'Take the ethernet adapter out first: the phone has one port. ' : ''}
              Waiting for the phone on USB…
            </p>
          )}
          {step === 'allow-usb' && (
            <p className="text-[13px] text-vx-warn">
              On the phone, tap <b>Allow</b> on &quot;Allow USB debugging?&quot; and tick &quot;Always allow from this
              computer&quot;.
            </p>
          )}
        </Step>
        <Step n={2} state={switchState} title="Switch on adb over the network">
          {switchState === 'active' &&
            (canSwitch ? (
              <Button variant="primary" onClick={() => setConfirming(true)}>
                Switch to ethernet
              </Button>
            ) : (
              <p className="text-[13px] text-vx-fg-muted">
                {ready ? 'Only operators can switch a phone to ethernet. Ask one to do this step.' : ''}
              </p>
            ))}
        </Step>
        <Step n={3} state={adapterState} title="Unplug USB and plug the ethernet adapter (with its charger) back in">
          {switched && (
            <>
              <p className="text-[13px] text-vx-fg-muted">Waiting for the phone on the phone network…</p>
              {onUSB && canSwitch && (
                <Button variant="quiet" size="sm" onClick={() => setConfirming(true)}>
                  Still on USB after a while? Switch again
                </Button>
              )}
            </>
          )}
        </Step>
      </ol>

      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={`Switch ${name} to ethernet?`}
        consequence="Batter removes the old USB tether app (gnirehtet) from the phone and switches its adb to the network. Then unplug USB and plug in the ethernet adapter: the phone moves to the phone network and reaches the internet only through its VPN profile."
        confirmLabel="Switch"
        tone="primary"
        formatError={(err) => errorMessage(err, 'switch the phone to the network')}
        onConfirm={switchToEthernet}
      />
    </div>
  );
}
