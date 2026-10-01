'use client';

import { useEffect, useState } from 'react';
import { updateDevice, deleteDevice, setTether, listVPNProfileNames, DeviceInfo, VPNProfileName } from '@/lib/api';
import { isOperator } from '@/lib/auth';
import { networkLabel } from '@/lib/device-network';
import { EthernetPanel } from './ethernet-panel';

interface DeviceEditModalProps {
  device: DeviceInfo | null;
  onClose: () => void;
  onSaved: () => void;
  onDeleted: () => void;
}

export function DeviceEditModal({ device, onClose, onSaved, onDeleted }: DeviceEditModalProps) {
  const [nickname, setNickname] = useState(device?.nickname || '');
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [profileId, setProfileId] = useState(device?.vpn_profile_id ?? '');
  const [profiles, setProfiles] = useState<VPNProfileName[] | null>(null);
  const [tetherBusy, setTetherBusy] = useState(false);
  const [tetherNote, setTetherNote] = useState('');

  useEffect(() => {
    listVPNProfileNames()
      .then(setProfiles)
      .catch(() => setProfiles([]));
  }, []);

  if (!device) return null;

  const handleTether = async (id: string) => {
    setTetherBusy(true);
    setTetherNote('');
    try {
      const res = await setTether(device.serial, id || null);
      setProfileId(res.vpn_profile_id ?? '');
      if (res.apply_error) {
        setTetherNote(`Saved, but not applied yet: ${res.apply_error}`);
      }
    } catch (err) {
      setTetherNote(err instanceof Error ? err.message : 'Failed to change the phone\'s internet');
    } finally {
      setTetherBusy(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateDevice(device.serial, { nickname });
      onSaved();
    } catch {
      // ignore
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteDevice(device.serial);
      onDeleted();
    } catch {
      // ignore
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="bg-gray-900 border border-gray-800 rounded-xl w-full max-w-md mx-4 shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-white">Edit Device</h3>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-gray-300 text-lg leading-none"
          >
            &times;
          </button>
        </div>

        {/* Content */}
        <div className="px-5 py-4 space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-[10px] text-gray-500 mb-1">Serial</label>
              <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5 font-mono">
                {device.serial}
              </div>
            </div>
            <div>
              <label className="block text-[10px] text-gray-500 mb-1">Model</label>
              <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5">
                {device.model || 'Unknown'}
              </div>
            </div>
            <div>
              <label className="block text-[10px] text-gray-500 mb-1">Product</label>
              <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5">
                {device.product || 'Unknown'}
              </div>
            </div>
            <div>
              <label className="block text-[10px] text-gray-500 mb-1">Android Version</label>
              <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5">
                {device.android_version || 'Unknown'}
              </div>
            </div>
          </div>

          <div>
            <label className="block text-xs text-gray-400 mb-1.5">Nickname</label>
            <input
              type="text"
              value={nickname}
              onChange={(e) => setNickname(e.target.value)}
              placeholder="e.g. Test Phone 1"
              className="w-full px-3 py-2 text-xs bg-gray-800 border border-gray-700 rounded-lg text-gray-200 placeholder-gray-600 focus:outline-none focus:border-brand-500"
            />
          </div>

          <div>
            <label htmlFor="internet-sharing" className="block text-xs text-gray-400 mb-1.5">
              Internet
            </label>
            <select
              id="internet-sharing"
              value={profileId}
              onChange={(e) => handleTether(e.target.value)}
              disabled={tetherBusy || profiles === null || (profiles.length === 0 && !profileId)}
              className="w-full px-3 py-2 text-xs bg-gray-800 border border-gray-700 rounded-lg text-gray-200 focus:outline-none focus:border-brand-500 disabled:opacity-50"
            >
              <option value="">None</option>
              {profiles?.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <p className="mt-1 text-[10px] text-gray-500">
              {profiles !== null && profiles.length === 0
                ? 'No VPN profiles yet. Ask an admin to add one to give the phone internet.'
                : 'On the phone network (ethernet adapter), the phone reaches the internet only through the chosen VPN, never directly. None: no internet.'}
            </p>
            {tetherNote && <p className="mt-1 text-[10px] text-yellow-400">{tetherNote}</p>}
          </div>

          <div>
            <label className="block text-xs text-gray-400 mb-1.5">Connection</label>
            <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5 mb-2">{networkLabel(device)}</div>
            {isOperator() && (device.connection !== 'lan' || device.needs_reprovision) && (
              <EthernetPanel serial={device.serial} device={device} />
            )}
          </div>

          {/* Delete section */}
          <div className="pt-3 border-t border-gray-800">
            {!confirmDelete ? (
              <button
                onClick={() => setConfirmDelete(true)}
                className="text-xs text-red-400 hover:text-red-300"
              >
                Delete Device
              </button>
            ) : (
              <div className="flex items-center gap-2">
                <span className="text-xs text-red-400">Are you sure?</span>
                <button
                  onClick={handleDelete}
                  disabled={deleting}
                  className="px-3 py-2 text-xs bg-red-600 hover:bg-red-700 text-white rounded-lg disabled:opacity-50"
                >
                  {deleting ? 'Deleting...' : 'Confirm Delete'}
                </button>
                <button
                  onClick={() => setConfirmDelete(false)}
                  className="px-3 py-2 text-xs text-gray-400 hover:text-gray-200"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-gray-800">
          <button
            onClick={onClose}
            className="px-3 py-2 text-xs text-gray-400 hover:text-gray-200"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}
