// Pure helpers for the phone network admin page.

export interface NetworkState {
  state: string;
  port?: { mac: string; name: string };
  error?: string;
  unavailable?: string;
}

export interface NICLike {
  name: string;
  up: boolean;
  carrier: boolean;
  driver?: string;
  bus?: string;
  usable: boolean;
  in_use?: boolean;
}

export function stateSummary(s: NetworkState): { label: string; tone: 'ok' | 'off' | 'bad' } {
  switch (s.state) {
    case 'active':
      return { label: `On, using ${s.port?.name ?? 'its port'}`, tone: 'ok' };
    case 'off':
      return { label: 'Off: phones get no internet', tone: 'off' };
    case 'missing':
      return { label: s.port?.name ? `Port ${s.port.name} isn't on the box` : "The chosen port isn't on the box", tone: 'bad' };
    case 'unavailable':
      return { label: 'Unavailable on this server', tone: 'bad' };
    default:
      return { label: 'Not working', tone: 'bad' };
  }
}

// One line describing a port in the picker: link, driver, bus.
export function describeNIC(n: NICLike): string {
  const link = n.in_use ? 'in use by Batter' : n.carrier ? 'cable connected' : n.up ? 'up, no cable' : 'no cable';
  const hw = [n.driver, n.bus?.toUpperCase()].filter(Boolean).join(', ');
  return hw ? `${link} · ${hw}` : link;
}

// Ports the admin may pick: usable ones, the current one first.
export function pickable<T extends NICLike>(nics: T[]): T[] {
  return nics.filter((n) => n.usable).sort((a, b) => Number(!!b.in_use) - Number(!!a.in_use));
}
