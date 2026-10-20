'use client';

import { DeviceInfo } from '@/lib/api';
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="bg-gray-900 border border-gray-800 rounded-xl w-full max-w-md mx-4 shadow-2xl">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-white">
            Reconnect {device.nickname || device.model || device.serial}
          </h3>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-300 text-lg leading-none">
            &times;
          </button>
        </div>
        <div className="px-5 py-4">
          <EthernetPanel serial={device.serial} device={device} onDone={onDone} />
        </div>
        <div className="flex justify-end px-5 py-3 border-t border-gray-800">
          <button
            onClick={onClose}
            className="px-2.5 py-1.5 rounded border border-gray-700 text-gray-300 text-xs hover:bg-gray-800"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
