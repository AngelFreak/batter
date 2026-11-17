'use client';

import { DeviceInfo } from '@/lib/api';
import { displayName } from '@/lib/dashboard-filter';
import { Button, Dialog } from '@/components/ui';
import { EthernetPanel } from './ethernet-panel';

/** Brings a restarted phone back onto the phone network, step by step. */
export function ReconnectModal({
  device,
  onClose,
  onDone,
}: {
  device: DeviceInfo;
  onClose: () => void;
  onDone: () => void;
}) {
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Reconnect ${displayName(device)}`}
      description="Bring the phone back onto the phone network."
      footer={
        <Button variant="quiet" onClick={onClose}>
          Close
        </Button>
      }
    >
      <EthernetPanel serial={device.serial} device={device} onDone={onDone} />
    </Dialog>
  );
}
