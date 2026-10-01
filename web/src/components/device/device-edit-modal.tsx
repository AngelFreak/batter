'use client';

import { useState } from 'react';
import { updateDevice, deleteDevice, setTether, DeviceInfo } from '@/lib/api';

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
  const [tether, setTetherState] = useState(device?.reverse_tether ?? false);
  const [tetherBusy, setTetherBusy] = useState(false);
  const [tetherNote, setTetherNote] = useState('');

  if (!device) return null;

  const handleTether = async (enabled: boolean) => {
    setTetherBusy(true);
    setTetherNote('');
    try {
      const res = await setTether(device.serial, enabled);
      setTetherState(res.reverse_tether);
      if (res.apply_error) {
        setTetherNote(`Saved; will apply when the device is connected (${res.apply_error})`);
      }
    } catch (err) {
      setTetherNote(err instanceof Error ? err.message : 'Failed to change reverse tethering');
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
            <div className="flex items-center justify-between">
              <label htmlFor="reverse-tether" className="text-xs text-gray-400">
                Reverse tethering
              </label>
              <button
                id="reverse-tether"
                role="switch"
                aria-checked={tether}
                onClick={() => handleTether(!tether)}
                disabled={tetherBusy}
                className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-50 ${
                  tether ? 'bg-brand-600' : 'bg-gray-700'
                }`}
              >
                <span
                  className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${
                    tether ? 'translate-x-4' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </div>
            <p className="mt-1 text-[10px] text-gray-500">
              The phone uses this server&apos;s internet over USB. The first time, the phone asks
              for VPN permission: accept it in the live view.
            </p>
            {tetherNote && <p className="mt-1 text-[10px] text-yellow-400">{tetherNote}</p>}
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
