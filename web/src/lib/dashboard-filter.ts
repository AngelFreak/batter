// The dashboard's tiles, filter, search and empty states, computed from the
// device list the page already polls.

import type { DeviceInfo } from './api';
import { deviceStatus } from './device-status';

export type StatusFilter = 'all' | 'streaming' | 'idle' | 'attention' | 'offline';

type Device = Pick<
  DeviceInfo,
  'serial' | 'status' | 'has_session' | 'needs_reprovision' | 'nickname' | 'model' | 'product'
>;

export function statusBucket(d: Device): Exclude<StatusFilter, 'all'> {
  const s = deviceStatus(d);
  if (s.tone === 'live') return 'streaming';
  if (s.tone === 'warn' || s.tone === 'danger') return 'attention';
  if (s.code === 'connected') return 'idle';
  return 'offline';
}

export type StatusCounts = Record<StatusFilter, number>;

export function countByStatus(devices: Device[]): StatusCounts {
  const counts: StatusCounts = { all: devices.length, streaming: 0, idle: 0, attention: 0, offline: 0 };
  for (const d of devices) counts[statusBucket(d)]++;
  return counts;
}

/** The name a person knows the phone by. */
export function displayName(d: Pick<Device, 'serial' | 'nickname' | 'model'>): string {
  return d.nickname || d.model || d.serial;
}

export type SortKey = 'name' | 'status';

const BUCKET_ORDER: Record<Exclude<StatusFilter, 'all'>, number> = { attention: 0, streaming: 1, idle: 2, offline: 3 };

export interface FilterOptions {
  status: StatusFilter;
  query: string;
  sort: SortKey;
}

export function filterDevices<T extends Device>(devices: T[], opts: FilterOptions): T[] {
  const q = opts.query.trim().toLowerCase();
  const out = devices.filter((d) => {
    if (opts.status !== 'all' && statusBucket(d) !== opts.status) return false;
    if (!q) return true;
    return [d.nickname, d.model, d.product, d.serial].some((f) => f?.toLowerCase().includes(q));
  });
  const byName = (a: T, b: T) => displayName(a).localeCompare(displayName(b), undefined, { numeric: true });
  out.sort(
    opts.sort === 'status'
      ? (a, b) => BUCKET_ORDER[statusBucket(a)] - BUCKET_ORDER[statusBucket(b)] || byName(a, b)
      : byName,
  );
  return out;
}

export type EmptyReason =
  | { kind: 'no-devices'; canAdd: boolean }
  | { kind: 'group-empty'; group: string }
  | { kind: 'no-match'; query: string; status: StatusFilter }
  | null;

/** Why the grid is empty, so the page can say what to do. Null if it isn't. */
export function emptyReason(args: {
  total: number;
  inGroup: number;
  shown: number;
  group?: string;
  query: string;
  status: StatusFilter;
  canAdd: boolean;
}): EmptyReason {
  if (args.shown > 0) return null;
  if (args.total === 0) return { kind: 'no-devices', canAdd: args.canAdd };
  if (args.group && args.inGroup === 0) return { kind: 'group-empty', group: args.group };
  return { kind: 'no-match', query: args.query.trim(), status: args.status };
}
