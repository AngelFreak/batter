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
