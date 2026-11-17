'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter, useParams } from 'next/navigation';
import { LoaderCircle, MonitorOff, Play, RotateCw, Square, TriangleAlert } from 'lucide-react';
import { DeviceViewer } from '@/components/device/device-viewer';
import { EthernetPanel } from '@/components/device/ethernet-panel';
import { BackButton } from '@/components/viewer/back-button';
import { ViewerHeader } from '@/components/viewer/viewer-header';
import type { ViewerLive } from '@/components/viewer/viewer-status';
import { Button, ConfirmDialog, EmptyState, useToast } from '@/components/ui';
import { getDevice, startSession, stopSession, upgradeSession, downgradeSession, DeviceInfo } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { getToken } from '@/lib/auth';
import { displayName } from '@/lib/dashboard-filter';
import { networkState } from '@/lib/device-network';
import { useUser } from '@/lib/use-user';
import { QUALITY_LEVELS, storedQuality, storeQuality, upgradeAtStoredQuality, type VideoQuality } from '@/lib/video-quality';

/** How long starting a session may take before the page offers Retry. */
const SLOW_START_MS = 15_000;

export default function DeviceDetailPage() {
  const router = useRouter();
  const params = useParams();
  const toast = useToast();
  const { isOperator } = useUser();
  const serial = decodeURIComponent(params.serial as string);
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [slow, setSlow] = useState(false);
  const [error, setError] = useState('');
  const [quality, setQuality] = useState<VideoQuality>('medium');
  const [changingQuality, setChangingQuality] = useState(false);
  // A restarted phone can't be controlled until it's reconnected over USB;
  // the steps stay up until it's back, then the session starts.
  const [reconnecting, setReconnecting] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [live, setLive] = useState<ViewerLive | null>(null);
  const [confirmStop, setConfirmStop] = useState(false);

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login');
      return;
    }
  }, [router]);

  useEffect(() => {
    const init = async () => {
      setLoading(true);
      setError('');
      try {
        const d = await getDevice(serial);
        setDevice(d);
        if (networkState(d) === 'needs-reprovision') {
          setReconnecting(true);
          return;
        }

        // Auto-start and upgrade session for full-quality viewing
        if (!d.has_session) {
          await startSession(serial);
        }
        await upgradeAtStoredQuality(serial, upgradeSession);

        // Refresh device info
        const updated = await getDevice(serial);
        setDevice(updated);
      } catch (err) {
        setError(errorMessage(err, 'start the session'));
      } finally {
        setLoading(false);
      }
    };

    init();

    return () => {
      // Downgrade session when leaving the page
      downgradeSession(serial).catch(() => {});
    };
  }, [serial, attempt]);

  // Starting the session is taking long: offer Retry rather than a spinner for ever.
  useEffect(() => {
    if (!loading) {
      setSlow(false);
      return;
    }
    const t = setTimeout(() => setSlow(true), SLOW_START_MS);
    return () => clearTimeout(t);
  }, [loading, attempt]);

  const reconnected = useCallback(() => {
    setReconnecting(false);
    setAttempt((a) => a + 1);
  }, []);

  useEffect(() => setQuality(storedQuality()), []);

  // The session is shared, so someone else may change its level; keep the
  // shown level current. A background poll stays quiet when it fails.
  useEffect(() => {
    const t = setInterval(() => {
      getDevice(serial).then(setDevice).catch(() => {});
    }, 5000);
    return () => clearInterval(t);
  }, [serial]);

  const name = device ? displayName(device) : serial;

  const handleQuality = async (q: VideoQuality) => {
    setQuality(q);
    storeQuality(q);
    setChangingQuality(true);
    try {
      await upgradeSession(serial, { quality: q, change: true });
      setDevice(await getDevice(serial));
    } catch (err) {
      toast.error(errorMessage(err, 'change the quality'));
    } finally {
      setChangingQuality(false);
    }
  };

  const sessionQuality = device?.session_quality;

  // The stream drops when the backend restarts (sessions don't survive it).
  // Start a fresh session before the viewer reconnects; this is a no-op on
  // the server if the session is still alive.
  const ensureSession = useCallback(() => startSession(serial), [serial]);

  // A failure shows as a toast and leaves the session as it was.
  const handleStop = async () => {
    try {
      await stopSession(serial);
    } catch (err) {
      toast.error(errorMessage(err, 'stop the session'));
      return;
    }
    toast.success(`Session on ${name} stopped`);
    router.push('/dashboard');
  };

  const showViewer = !reconnecting && !loading && !error && !!device?.has_session;

  const headerActions = showViewer && (
    <>
      <label className="flex items-center gap-2">
        <span className="vx-label">Quality</span>
        <select
          value={quality}
          onChange={(e) => handleQuality(e.target.value as VideoQuality)}
          disabled={changingQuality}
          className="vx-input !h-9 !w-auto !px-2.5 !text-[13px] disabled:opacity-50"
        >
          {QUALITY_LEVELS.map((l) => (
            <option key={l.value} value={l.value}>
              {l.label}
            </option>
          ))}
        </select>
      </label>
      {sessionQuality && sessionQuality !== quality && !changingQuality && (
        <span
          className="font-mono text-[11px] uppercase tracking-wide text-vx-warn"
          title="The session is shared; the latest choice applies to everyone watching."
        >
          Now {sessionQuality}, changed by another viewer
        </span>
      )}
      <Button variant="danger" icon={<Square size={14} />} onClick={() => setConfirmStop(true)}>
        Stop session
      </Button>
    </>
  );

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <ViewerHeader name={name} serial={serial} live={showViewer ? (live ?? undefined) : undefined}>
        {headerActions}
      </ViewerHeader>

      {reconnecting && device ? (
        <div className="flex flex-1 items-center justify-center p-4">
          <section className="vx-panel w-full max-w-md space-y-4 p-5" aria-labelledby="restarted-title">
            <div className="flex items-center gap-2 text-vx-warn">
              <TriangleAlert size={16} aria-hidden />
              <h2 id="restarted-title" className="text-lg text-vx-fg">
                This phone restarted
              </h2>
            </div>
            {isOperator ? (
              <EthernetPanel serial={serial} device={device} onDone={reconnected} />
            ) : (
              <p className="text-sm text-vx-fg-muted">
                A restart turns adb over the network off, so Batter can&apos;t show or control the phone until it is
                plugged into USB once. Ask an operator to reconnect it.
              </p>
            )}
            <BackButton />
          </section>
        </div>
      ) : loading ? (
        <div className="flex flex-1 items-center justify-center p-4" role="status">
          {slow ? (
            <EmptyState
              icon={<LoaderCircle className="animate-spin" />}
              title="Still starting…"
              action={
                <div className="flex flex-wrap justify-center gap-2">
                  <Button variant="primary" size="lg" icon={<RotateCw size={16} />} onClick={() => setAttempt((a) => a + 1)}>
                    Retry
                  </Button>
                  <BackButton />
                </div>
              }
            >
              Starting the session on {name} is taking longer than usual. The phone may be busy. You can wait, retry, or
              go back.
            </EmptyState>
          ) : (
            <p className="inline-flex items-center gap-2 text-sm text-vx-fg-muted">
              <LoaderCircle size={16} className="animate-spin text-vx-accent" aria-hidden />
              Starting session…
            </p>
          )}
        </div>
      ) : error ? (
        <div className="flex flex-1 items-center justify-center p-4">
          <EmptyState
            icon={<TriangleAlert />}
            title="Couldn't open this phone"
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <Button variant="primary" size="lg" icon={<RotateCw size={16} />} onClick={() => setAttempt((a) => a + 1)}>
                  Retry
                </Button>
                <BackButton />
              </div>
            }
          >
            {error}
          </EmptyState>
        </div>
      ) : device?.has_session ? (
        <DeviceViewer serial={serial} onStreamLost={ensureSession} onLiveChange={setLive} />
      ) : (
        <div className="flex flex-1 items-center justify-center p-4">
          <EmptyState
            icon={<MonitorOff />}
            title="No session running"
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <Button variant="primary" size="lg" icon={<Play size={16} />} onClick={() => setAttempt((a) => a + 1)}>
                  Start session
                </Button>
                <BackButton />
              </div>
            }
          >
            There is no live session on {name}. It may have been stopped. Start one to watch and control the phone.
          </EmptyState>
        </div>
      )}

      <ConfirmDialog
        open={confirmStop}
        onClose={() => setConfirmStop(false)}
        title={`Stop the session on ${name}?`}
        consequence="It ends for everyone watching this phone. The phone keeps running."
        confirmLabel="Stop for everyone"
        onConfirm={handleStop}
      />
    </div>
  );
}
