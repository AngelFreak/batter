'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter, useParams } from 'next/navigation';
import { DeviceViewer } from '@/components/device/device-viewer';
import { getDevice, startSession, stopSession, upgradeSession, downgradeSession, DeviceInfo } from '@/lib/api';
import { getToken } from '@/lib/auth';

export default function DeviceDetailPage() {
  const router = useRouter();
  const params = useParams();
  const serial = decodeURIComponent(params.serial as string);
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login');
      return;
    }
  }, [router]);

  useEffect(() => {
    const init = async () => {
      try {
        const d = await getDevice(serial);
        setDevice(d);

        // Auto-start and upgrade session for full-quality viewing
        if (!d.has_session) {
          await startSession(serial);
        }
        await upgradeSession(serial);

        // Refresh device info
        const updated = await getDevice(serial);
        setDevice(updated);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load device');
      } finally {
        setLoading(false);
      }
    };

    init();

    return () => {
      // Downgrade session when leaving the page
      downgradeSession(serial).catch(() => {});
    };
  }, [serial]);

  // The stream drops when the backend restarts (sessions don't survive it).
  // Start a fresh session before the viewer reconnects; this is a no-op on
  // the server if the session is still alive.
  const ensureSession = useCallback(() => startSession(serial), [serial]);

  const handleStop = async () => {
    try {
      await stopSession(serial);
      router.push('/dashboard');
    } catch {
      // ignore
    }
  };

  return (
    <div className="flex flex-col h-full overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-2 border-b border-gray-800 bg-gray-900">
          <div className="flex items-center gap-3">
            <button
              onClick={() => router.push('/dashboard')}
              className="text-sm text-gray-400 hover:text-gray-200"
            >
              &larr; Back
            </button>
            <span className="text-sm font-medium text-white">
              {device?.model || serial}
            </span>
            <span className="text-[10px] text-gray-500">{serial}</span>
          </div>
          <button
            onClick={handleStop}
            className="px-3 py-2 text-xs bg-red-600/20 hover:bg-red-600/40 text-red-400 rounded-lg"
          >
            Stop Session
          </button>
        </div>

        {/* Viewer */}
        <div className="flex-1 overflow-hidden">
          {loading ? (
            <div className="flex items-center justify-center h-full text-gray-500 text-sm">
              Starting session...
            </div>
          ) : error ? (
            <div className="flex items-center justify-center h-full text-red-400 text-sm">
              {error}
            </div>
          ) : device?.has_session ? (
            <DeviceViewer serial={serial} onStreamLost={ensureSession} />
          ) : (
            <div className="flex items-center justify-center h-full text-gray-500 text-sm">
              No active session
            </div>
          )}
        </div>
    </div>
  );
}
