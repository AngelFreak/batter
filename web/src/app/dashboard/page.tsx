'use client';

import { useEffect, useState, useCallback, useMemo, useRef } from 'react';
import { useRouter } from 'next/navigation';
import { Play, Plus, RefreshCw, Search, SearchX, Smartphone, Users, WifiOff } from 'lucide-react';
import { DeviceGrid } from '@/components/device/device-grid';
import { DeviceWizard } from '@/components/device/device-wizard';
import { DeviceEditModal } from '@/components/device/device-edit-modal';
import { ReconnectModal } from '@/components/device/reconnect-modal';
import { Banner, Button, ConfirmDialog, EmptyState, PageHeader, StatusGlyph, Tile, cx, useToast } from '@/components/ui';
import { listDevices, startSession, deleteDevice, listGroups, getGroupDevices, DeviceInfo, DeviceGroup } from '@/lib/api';
import { getToken } from '@/lib/auth';
import { errorMessage } from '@/lib/api-error';
import {
  countByStatus,
  displayName,
  emptyReason,
  filterDevices,
  type SortKey,
  type StatusFilter,
} from '@/lib/dashboard-filter';
import { useUser } from '@/lib/use-user';

const GROUP_STORAGE_KEY = 'batter_dashboard_group';

const TILES: { key: StatusFilter; label: string; glyph?: { icon: 'dot' | 'ring' | 'triangle' | 'slash'; tone: string } }[] = [
  { key: 'all', label: 'All' },
  { key: 'streaming', label: 'Streaming', glyph: { icon: 'dot', tone: 'text-vx-accent' } },
  { key: 'idle', label: 'Idle', glyph: { icon: 'ring', tone: 'text-vx-fg-muted' } },
  { key: 'attention', label: 'Attention', glyph: { icon: 'triangle', tone: 'text-vx-warn' } },
  { key: 'offline', label: 'Offline', glyph: { icon: 'slash', tone: 'text-vx-fg-label' } },
];

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

export default function DashboardPage() {
  const router = useRouter();
  const toast = useToast();
  const { isOperator: canAddDevice } = useUser();

  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  // Full-page loading and the full-page error are for the very first load only.
  const [firstLoad, setFirstLoad] = useState<'loading' | 'error' | 'done'>('loading');
  const [loadError, setLoadError] = useState('');
  // A later refresh failed: keep the last list and say so.
  const [connectionLost, setConnectionLost] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const hasLoaded = useRef(false);

  // The wizard and edit modal are keyed so each run starts fresh; they stay
  // mounted while closed so the dialog can give focus back to the opener.
  const [wizardRun, setWizardRun] = useState(0);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [editRun, setEditRun] = useState(0);
  const [editSerial, setEditSerial] = useState<string | null>(null);
  const [reconnectSerial, setReconnectSerial] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeviceInfo | null>(null);
  const [startingAll, setStartingAll] = useState(false);

  // Filters
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<SortKey>('name');

  // Group filter state
  const [groups, setGroups] = useState<DeviceGroup[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState<string>('');
  const [groupSerials, setGroupSerials] = useState<string[] | null>(null);

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login');
      return;
    }
    // Restore persisted group filter
    const saved = sessionStorage.getItem(GROUP_STORAGE_KEY);
    if (saved) setSelectedGroupId(saved);
  }, [router]);

  const fetchDevices = useCallback(async () => {
    try {
      const data = await listDevices();
      setDevices(data);
      hasLoaded.current = true;
      setFirstLoad('done');
      setConnectionLost(false);
      setLoadError('');
    } catch (err) {
      if (hasLoaded.current) {
        // Keep the last good list on screen.
        setConnectionLost(true);
      } else {
        setLoadError(errorMessage(err, 'load the phones'));
        setFirstLoad('error');
      }
    }
  }, []);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await fetchDevices();
    } finally {
      setRefreshing(false);
    }
  }, [fetchDevices]);

  const retryFirstLoad = () => {
    setFirstLoad('loading');
    fetchDevices();
  };

  const fetchGroups = useCallback(async () => {
    try {
      const g = await listGroups();
      setGroups(g);
      // A remembered group that no longer exists would hide every phone.
      setSelectedGroupId((cur) => (cur && !g.some((x) => x.id === cur) ? '' : cur));
    } catch {
      // groups are optional, don't block dashboard
    }
  }, []);

  useEffect(() => {
    fetchDevices();
    fetchGroups();
    const interval = setInterval(fetchDevices, 5000);
    return () => clearInterval(interval);
  }, [fetchDevices, fetchGroups]);

  // When group selection changes, fetch its member serials
  useEffect(() => {
    if (!selectedGroupId) {
      setGroupSerials(null);
      sessionStorage.removeItem(GROUP_STORAGE_KEY);
      return;
    }
    sessionStorage.setItem(GROUP_STORAGE_KEY, selectedGroupId);
    getGroupDevices(selectedGroupId)
      .then((serials) => setGroupSerials(serials))
      .catch(() => setGroupSerials([]));
  }, [selectedGroupId]);

  const inGroup = useMemo(
    () => (groupSerials ? devices.filter((d) => groupSerials.includes(d.serial)) : devices),
    [devices, groupSerials],
  );
  const selectedGroup = groups.find((g) => g.id === selectedGroupId);

  const counts = useMemo(() => countByStatus(inGroup), [inGroup]);
  const shown = useMemo(
    () => filterDevices(inGroup, { status: statusFilter, query, sort }),
    [inGroup, statusFilter, query, sort],
  );
  const connectedCount = inGroup.filter((d) => d.status === 'connected').length;
  const startable = inGroup.filter((d) => d.status === 'connected' && !d.has_session);

  const restarted = inGroup.filter((d) => d.needs_reprovision);
  const unauthorized = inGroup.filter((d) => d.status === 'unauthorized' && !d.needs_reprovision);
  const reconnecting = devices.find((d) => d.serial === reconnectSerial);
  const editing = devices.find((d) => d.serial === editSerial) ?? null;

  const empty = emptyReason({
    total: devices.length,
    inGroup: inGroup.length,
    shown: shown.length,
    group: selectedGroup?.name,
    query,
    status: statusFilter,
    canAdd: canAddDevice,
  });

  const clearFilters = () => {
    setQuery('');
    setStatusFilter('all');
  };

  const openWizard = () => {
    setWizardRun((n) => n + 1);
    setWizardOpen(true);
  };

  const openEdit = (serial: string) => {
    setEditRun((n) => n + 1);
    setEditSerial(serial);
  };

  const handleStartAll = async () => {
    const targets = startable;
    if (targets.length === 0) return;
    setStartingAll(true);
    let ok = 0;
    const failed: string[] = [];
    let firstError: unknown = null;
    for (const d of targets) {
      try {
        await startSession(d.serial, 360, 5);
        ok++;
      } catch (err) {
        firstError ??= err;
        failed.push(displayName(d));
      }
    }
    setStartingAll(false);
    if (failed.length === 0) {
      toast.success(`Started ${plural(ok, 'session')}.`);
    } else {
      toast.error(
        `Started ${ok} of ${targets.length}. Not started: ${failed.join(', ')}. ${errorMessage(firstError, 'start a session')}`,
      );
    }
    fetchDevices();
  };

  const filtersActive = query.trim() !== '' || statusFilter !== 'all';

  return (
    <>
      <PageHeader
        title="Devices"
        sub={
          <>
            {selectedGroup ? (
              <span>
                {plural(inGroup.length, 'phone')} in &quot;{selectedGroup.name}&quot;
              </span>
            ) : (
              <span>{counts.all} registered</span>
            )}
            <span aria-hidden>·</span>
            <span>{connectedCount} connected</span>
            <span aria-hidden>·</span>
            <span>{counts.streaming} streaming</span>
          </>
        }
        actions={
          <>
            <Button
              onClick={refresh}
              disabled={refreshing}
              icon={<RefreshCw size={14} className={refreshing ? 'animate-spin' : undefined} />}
            >
              Refresh
            </Button>
            {startable.length > 0 && (
              <Button onClick={handleStartAll} disabled={startingAll} icon={<Play size={14} />}>
                {startingAll ? 'Starting…' : `Start ${plural(startable.length, 'session')}`}
              </Button>
            )}
            {canAddDevice && (
              <Button variant="primary" icon={<Plus size={14} />} onClick={openWizard}>
                Add device
              </Button>
            )}
          </>
        }
      />

      <div className="flex flex-col gap-4 px-4 py-5 sm:px-6">
        {firstLoad === 'loading' && (
          <div role="status" aria-label="Loading phones" className="flex flex-col gap-4">
            <div className="grid grid-cols-2 gap-px border border-vx-hairline bg-vx-hairline sm:grid-cols-5">
              {TILES.map((t, i) => (
                <div key={t.key} className={cx('min-h-[86px] animate-pulse bg-vx-surface', i === 0 && 'col-span-2 sm:col-span-1')} />
              ))}
            </div>
            <div className="grid grid-cols-2 border-l border-t border-vx-hairline md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
              {Array.from({ length: 6 }, (_, i) => (
                <div key={i} className="aspect-[9/16] animate-pulse border-b border-r border-vx-hairline bg-vx-surface" />
              ))}
            </div>
          </div>
        )}

        {firstLoad === 'error' && (
          <EmptyState
            icon={<WifiOff />}
            title="Couldn't load the phones"
            action={
              <Button variant="primary" icon={<RefreshCw size={14} />} onClick={retryFirstLoad}>
                Retry
              </Button>
            }
          >
            {loadError}
          </EmptyState>
        )}

        {firstLoad === 'done' && (
          <>
            {connectionLost && (
              <Banner tone="danger">Connection lost. Showing the last list; retrying.</Banner>
            )}

            {(restarted.length > 0 || unauthorized.length > 0) && (
              <Banner
                tone="warn"
                actions={
                  canAddDevice && restarted.length > 0
                    ? restarted.map((d) => (
                        <Button key={d.serial} size="sm" onClick={() => setReconnectSerial(d.serial)}>
                          Reconnect {displayName(d)}
                        </Button>
                      ))
                    : undefined
                }
              >
                <div className="flex flex-col gap-1">
                  {restarted.length > 0 && (
                    <p>
                      {plural(restarted.length, 'phone')} restarted and can&apos;t be controlled until{' '}
                      {restarted.length === 1 ? 'it is' : 'they are'} plugged into USB once.
                      {!canAddDevice && ` Ask an operator to reconnect ${restarted.length === 1 ? 'it' : 'them'}.`}
                    </p>
                  )}
                  {unauthorized.length > 0 && (
                    <p>
                      {plural(unauthorized.length, 'phone')} need{unauthorized.length === 1 ? 's' : ''} approval. Accept
                      &quot;Allow USB debugging?&quot; on {unauthorized.length === 1 ? 'the phone' : 'each phone'}.
                    </p>
                  )}
                </div>
              </Banner>
            )}

            {/* Status tiles: counts that also filter */}
            <div
              role="group"
              aria-label="Filter by status"
              className="grid grid-cols-2 gap-px border border-vx-hairline bg-vx-hairline sm:grid-cols-5"
            >
              {TILES.map((t, i) => (
                <Tile
                  key={t.key}
                  className={i === 0 ? 'col-span-2 sm:col-span-1' : undefined}
                  label={
                    <>
                      {t.glyph && <StatusGlyph icon={t.glyph.icon} className={t.glyph.tone} />}
                      <span className="text-vx-fg-label">{t.label}</span>
                    </>
                  }
                  value={counts[t.key]}
                  pressed={statusFilter === t.key}
                  onClick={() => setStatusFilter(statusFilter === t.key ? 'all' : t.key)}
                />
              ))}
            </div>

            {/* Toolbar */}
            <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
              <div className="relative col-span-2 sm:w-72">
                <Search
                  size={16}
                  aria-hidden
                  className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-vx-fg-label"
                />
                <input
                  type="search"
                  aria-label="Search phones"
                  placeholder="Search name, model or serial"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  className="vx-input pl-10"
                />
              </div>
              {groups.length > 0 && (
                <select
                  aria-label="Group"
                  value={selectedGroupId}
                  onChange={(e) => setSelectedGroupId(e.target.value)}
                  className="vx-input sm:w-auto"
                >
                  <option value="">All devices</option>
                  {groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              )}
              <select
                aria-label="Sort by"
                value={sort}
                onChange={(e) => setSort(e.target.value as SortKey)}
                className={cx('vx-input sm:w-auto', groups.length === 0 && 'col-span-2')}
              >
                <option value="name">Sort: name</option>
                <option value="status">Sort: status</option>
              </select>
              {filtersActive && (
                <Button variant="quiet" onClick={clearFilters} className="col-span-2 sm:col-span-1">
                  Clear filters
                </Button>
              )}
              {filtersActive && (
                <span className="vx-label sm:ml-auto" aria-live="polite">
                  {shown.length} of {inGroup.length} shown
                </span>
              )}
            </div>

            {empty?.kind === 'no-devices' && (
              <EmptyState
                icon={<Smartphone />}
                title="No phones yet"
                action={
                  empty.canAdd ? (
                    <Button variant="primary" icon={<Plus size={14} />} onClick={openWizard}>
                      Add device
                    </Button>
                  ) : undefined
                }
              >
                {empty.canAdd ? 'Register a phone to see it here.' : 'Ask an admin to give you access.'}
              </EmptyState>
            )}

            {empty?.kind === 'group-empty' && (
              <EmptyState
                icon={<Users />}
                title={`No phones in ${empty.group}`}
                action={<Button onClick={() => setSelectedGroupId('')}>Show all devices</Button>}
              >
                Add phones to this group on the Groups page, or pick another group.
              </EmptyState>
            )}

            {empty?.kind === 'no-match' && (
              <EmptyState
                icon={<SearchX />}
                title={empty.query ? `No phones match "${empty.query}"` : 'No phones in this status'}
                action={<Button onClick={clearFilters}>Clear filters</Button>}
              >
                {empty.query ? 'Check the spelling, or search by model or serial.' : 'Pick another status, or clear the filter.'}
              </EmptyState>
            )}

            <DeviceGrid
              devices={shown}
              onEdit={openEdit}
              onDelete={(serial) => setDeleteTarget(devices.find((d) => d.serial === serial) ?? null)}
              onReconnect={canAddDevice ? setReconnectSerial : undefined}
            />
          </>
        )}
      </div>

      {wizardRun > 0 && (
        <DeviceWizard
          key={wizardRun}
          open={wizardOpen}
          onClose={() => setWizardOpen(false)}
          onComplete={() => {
            setWizardOpen(false);
            fetchDevices();
          }}
        />
      )}

      {reconnecting && (
        <ReconnectModal
          device={reconnecting}
          onClose={() => {
            setReconnectSerial(null);
            fetchDevices();
          }}
          onDone={fetchDevices}
        />
      )}

      {editRun > 0 && (
        <DeviceEditModal
          key={editRun}
          device={editing}
          onClose={() => setEditSerial(null)}
          onSaved={() => {
            setEditSerial(null);
            fetchDevices();
          }}
          onDeleted={() => {
            setEditSerial(null);
            fetchDevices();
          }}
        />
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={`Remove ${deleteTarget ? displayName(deleteTarget) : 'this phone'} from Batter?`}
        consequence="The phone itself is not touched. It can be registered again."
        confirmLabel="Remove"
        formatError={(err) => errorMessage(err, 'remove the phone')}
        onConfirm={async () => {
          if (!deleteTarget) return;
          const name = displayName(deleteTarget);
          await deleteDevice(deleteTarget.serial);
          toast.success(`Removed ${name}.`);
          fetchDevices();
        }}
      />
    </>
  );
}
