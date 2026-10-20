// How a phone is attached: over USB, or on the phone network through its
// ethernet adapter (adb over TCP).

export interface DeviceNetwork {
  status: string;
  connection?: 'usb' | 'lan';
  lan_address?: string;
  needs_reprovision?: boolean;
}

export type NetworkState = 'usb' | 'ethernet' | 'needs-reprovision' | 'offline';

export function networkState(d: DeviceNetwork): NetworkState {
  if (d.needs_reprovision) return 'needs-reprovision';
  if (d.status !== 'connected') return 'offline';
  return d.connection === 'lan' ? 'ethernet' : 'usb';
}

export function networkLabel(d: DeviceNetwork): string {
  switch (networkState(d)) {
    case 'ethernet':
      return d.lan_address ? `Ethernet · ${d.lan_address}` : 'Ethernet';
    case 'usb':
      return 'USB';
    case 'needs-reprovision':
      return 'Needs USB re-provision';
    default:
      return 'Not connected';
  }
}

// True once a phone being moved to its adapter is driven over the network.
export function arrivedOnLAN(d: DeviceNetwork): boolean {
  return networkState(d) === 'ethernet';
}

// Where a phone is in moving to (or, after a restart, back onto) its
// ethernet adapter. A restart turns adb over the network off, and only a
// computer on USB can turn it on again:
//   plug-usb   not on USB yet (on a restarted phone the adapter is still in)
//   allow-usb  on USB, waiting for "Allow USB debugging?" to be accepted
//   switch     on USB: adb over the network can be switched on
//   done       driven over the network
export type ReconnectStep = 'plug-usb' | 'allow-usb' | 'switch' | 'done';

export function reconnectStep(d: DeviceNetwork): ReconnectStep {
  switch (networkState(d)) {
    case 'ethernet':
      return 'done';
    case 'usb':
      return 'switch';
    default:
      return d.status === 'unauthorized' ? 'allow-usb' : 'plug-usb';
  }
}
