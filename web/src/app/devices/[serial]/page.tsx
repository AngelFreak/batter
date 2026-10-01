'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter, useParams } from 'next/navigation';
import { DeviceViewer } from '@/components/device/device-viewer';
import { getDevice, startSession, stopSession, upgradeSession, downgradeSession, DeviceInfo } from '@/lib/api';
import { getToken } from '@/lib/auth';
import { QUALITY_LEVELS, storedQuality, storeQuality, upgradeAtStoredQuality, type VideoQuality } from '@/lib/video-quality';

export default function DeviceDetailPage() {
  const router = useRouter();
  const params = useParams();
  const serial = decodeURIComponent(params.serial as string);
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [quality, setQuality] = useState<VideoQuality>('medium');
  const [changingQuality, setChangingQuality] = useState(false);

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
        await upgradeAtStoredQuality(serial, upgradeSession);

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

  useEffect(() => setQuality(storedQuality()), []);

  // The session is shared, so someone else may change its level; keep the
  // shown level current.
  useEffect(() => {
    const t = setInterval(() => {
      getDevice(serial).then(setDevice).catch(() => {});
    }, 5000);
    return () => clearInterval(t);
  }, [serial]);

  const handleQuality = async (q: VideoQuality) => {
    setQuality(q);
    storeQuality(q);
    setChangingQuality(true);
    try {
      await upgradeSession(serial, { quality: q, change: true });
      setDevice(await getDevice(serial));
    } catch {
      // The viewer reconnects on its own; the shown level updates on poll.
    } finally {
      setChangingQuality(false);
    }
  };

  const sessionQuality = device?.session_quality;

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
          <div className="flex items-center gap-2">
          <label className="flex items-center gap-1.5 text-[10px] text-gray-400">
            Quality
            <select
              value={quality}
              onChange={(e) => handleQuality(e.target.value as VideoQuality)}
              disabled={changingQuality}
              className="px-2 py-1 text-[10px] bg-gray-800 border border-gray-700 rounded text-gray-200 disabled:opacity-50"
            >
              {QUALITY_LEVELS.map((l) => (
                <option key={l.value} value={l.value}>{l.label}</option>
              ))}
            </select>
          </label>
          {sessionQuality && sessionQuality !== quality && !changingQuality && (
            <span className="text-[10px] text-yellow-400" title="The session is shared; the latest choice applies to everyone watching.">
              Now {sessionQuality} (changed by another viewer)
            </span>
          )}
          <button
            onClick={handleStop}
            className="px-3 py-2 text-xs bg-red-600/20 hover:bg-red-600/40 text-red-400 rounded-lg"
          >
            Stop Session
          </button>
          </div>
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
