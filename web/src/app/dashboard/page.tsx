'use client';

import { useEffect, useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import { DeviceGrid } from '@/components/device/device-grid';
import { DeviceWizard } from '@/components/device/device-wizard';
import { DeviceEditModal } from '@/components/device/device-edit-modal';
import { ReconnectModal } from '@/components/device/reconnect-modal';
import { listDevices, startSession, deleteDevice, listGroups, getGroupDevices, DeviceInfo, DeviceGroup } from '@/lib/api';
import { getToken, getUser } from '@/lib/auth';

const GROUP_STORAGE_KEY = 'batter_dashboard_group';

export default function DashboardPage() {
  const router = useRouter();
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showWizard, setShowWizard] = useState(false);
  const [editSerial, setEditSerial] = useState<string | null>(null);
  const [reconnectSerial, setReconnectSerial] = useState<string | null>(null);

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
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load devices');
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchGroups = useCallback(async () => {
    try {
      const g = await listGroups();
      setGroups(g);
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
      .then(serials => setGroupSerials(serials))
      .catch(() => setGroupSerials([]));
  }, [selectedGroupId]);

  const filteredDevices = groupSerials
    ? devices.filter(d => groupSerials.includes(d.serial))
    : devices;

  const selectedGroup = groups.find(g => g.id === selectedGroupId);

  const handleStartAll = async () => {
    const connected = filteredDevices.filter(d => d.status === 'connected' && !d.has_session);
    for (const d of connected) {
      try {
        await startSession(d.serial, 360, 5);
      } catch {
        // skip failed
      }
    }
    fetchDevices();
  };

  const handleDeviceClick = (serial: string) => {
    router.push(`/devices/${encodeURIComponent(serial)}`);
  };

  const handleEdit = (serial: string) => {
    setEditSerial(serial);
  };

  const handleDelete = async (serial: string) => {
    try {
      await deleteDevice(serial);
      fetchDevices();
    } catch {
      // ignore
    }
  };

  const [canAddDevice, setCanAddDevice] = useState(false);

  useEffect(() => {
    const u = getUser();
    setCanAddDevice(u?.role === 'admin' || u?.role === 'operator');
  }, []);

  const totalCount = filteredDevices.length;
  const connectedCount = filteredDevices.filter(d => d.status === 'connected').length;
  const sessionCount = filteredDevices.filter(d => d.has_session).length;
  const restarted = filteredDevices.filter(d => d.needs_reprovision);
  const reconnecting = devices.find(d => d.serial === reconnectSerial);

  return (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
          <div>
            <h2 className="text-lg font-semibold text-white">Devices</h2>
            <p className="text-xs text-gray-500">
              {selectedGroup
                ? `Showing ${totalCount} device${totalCount !== 1 ? 's' : ''} in "${selectedGroup.name}"`
                : `${totalCount} registered, ${connectedCount} connected, ${sessionCount} streaming`}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {/* Group filter dropdown */}
            {groups.length > 0 && (
              <select
                value={selectedGroupId}
                onChange={e => setSelectedGroupId(e.target.value)}
                className="px-2 py-2 text-xs bg-gray-800 border border-gray-700 text-gray-300 rounded-lg"
              >
                <option value="">All Devices</option>
                {groups.map(g => (
                  <option key={g.id} value={g.id}>{g.name}</option>
                ))}
              </select>
            )}
            <button
              onClick={fetchDevices}
              className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg"
            >
              Refresh
            </button>
            {canAddDevice && (
              <button
                onClick={() => setShowWizard(true)}
                className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg"
              >
                Add Device
              </button>
            )}
            {connectedCount > sessionCount && (
              <button
                onClick={handleStartAll}
                className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg"
              >
                Start All Sessions
              </button>
            )}
          </div>
        </div>

        {/* Content */}
        <div className="p-6">
          {loading ? (
            <div className="text-gray-500 text-sm">Loading devices...</div>
          ) : error ? (
            <div className="text-red-400 text-sm">{error}</div>
          ) : (
            <>
              {restarted.length > 0 && (
                <div className="mb-4 rounded-lg border border-yellow-700 bg-yellow-900/20 p-3">
                  <div className="text-xs text-yellow-300">
                    {restarted.length === 1 ? '1 phone' : `${restarted.length} phones`} restarted and can&apos;t be
                    controlled until {restarted.length === 1 ? 'it is' : 'they are'} plugged into USB once.
                    {!canAddDevice && ` Ask an operator to reconnect ${restarted.length === 1 ? 'it' : 'them'}.`}
                  </div>
                  {canAddDevice && (
                    <div className="mt-2 flex flex-wrap gap-2">
                      {restarted.map(d => (
                        <button
                          key={d.serial}
                          onClick={() => setReconnectSerial(d.serial)}
                          className="px-2.5 py-1.5 rounded bg-brand-600 hover:bg-brand-700 text-white text-xs"
                        >
                          Reconnect {d.nickname || d.model || d.serial}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
              <DeviceGrid
                devices={filteredDevices}
                onDeviceClick={handleDeviceClick}
                onEdit={handleEdit}
                onDelete={handleDelete}
                onReconnect={canAddDevice ? setReconnectSerial : undefined}
              />
            </>
          )}
        </div>

      {showWizard && (
        <DeviceWizard
          onClose={() => setShowWizard(false)}
          onComplete={() => {
            setShowWizard(false);
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

      {editSerial && (
        <DeviceEditModal
          device={devices.find(d => d.serial === editSerial) || null}
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
    </>
  );
}
