'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import { useRouter } from 'next/navigation';
import { Check, Keyboard, RefreshCw, Square } from 'lucide-react';
import { listDevices, DeviceInfo } from '@/lib/api';
import { getToken } from '@/lib/auth';
import { MultiplexerInputHandler } from '@/lib/device-input-multiplexer';
import { MultiplexerGrid } from '@/components/device/multiplexer-grid';
import { useNoSwipeNavigation } from '@/lib/use-no-swipe-navigation';
import { NavBar } from '@/components/device/nav-bar';
import type { NavAction } from '@/lib/device-nav';
import { displayName } from '@/lib/dashboard-filter';
import { LinkButton } from '@/components/wall/link-button';
import {
  Banner, Button, ConfirmDialog, EmptyState, Field, PageHeader, cx,
} from '@/components/ui';

const NAV_LABEL: Record<NavAction, string> = {
  back: 'Back',
  home: 'Home',
  recents: 'Recents',
  all_apps: 'All apps',
};

/** What is waiting for a yes: a nav press or a text, with the phone count when it was asked. */
type Pending =
  | { kind: 'nav'; action: NavAction; n: number }
  | { kind: 'text'; text: string; n: number };

const phones = (n: number) => `${n} phone${n !== 1 ? 's' : ''}`;

/** "A, B, C and 2 more": compact enough for a banner. */
function compactNames(names: string[], max = 4): string {
  if (names.length <= max) return names.join(', ');
  return `${names.slice(0, max).join(', ')} and ${names.length - max} more`;
}

export default function MultiplexerPage() {
  useNoSwipeNavigation();
  const router = useRouter();
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [connectionLost, setConnectionLost] = useState(false);
  const [selectedSerials, setSelectedSerials] = useState<Set<string>>(new Set());
  const [active, setActive] = useState(false);
  const [textValue, setTextValue] = useState('');
  const [showTextInput, setShowTextInput] = useState(false);
  // Control status per phone, as the input handler reports it.
  const [controlStatus, setControlStatus] = useState<Record<string, string>>({});
  const [pending, setPending] = useState<Pending | null>(null);
  const multiplexerRef = useRef<MultiplexerInputHandler | null>(null);
  const primaryCanvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    if (!getToken()) {
      router.replace('/login');
      return;
    }
  }, [router]);

  const fetchDevices = useCallback(async () => {
    try {
      const data = await listDevices();
      setDevices(data);
      setConnectionLost(false);
    } catch {
      // Keep the last good list on screen and say so.
      setConnectionLost(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchDevices();
    const interval = setInterval(fetchDevices, 5000);
    return () => clearInterval(interval);
  }, [fetchDevices]);

  const streamingDevices = devices.filter(d => d.has_session);
  const selectedDevices = streamingDevices.filter(d => selectedSerials.has(d.serial));
  // The phones that are taking input right now: shown, and control connected.
  const targets = active ? selectedDevices.filter(d => controlStatus[d.serial] === 'connected') : [];

  const toggleDevice = (serial: string) => {
    setSelectedSerials(prev => {
      const next = new Set(prev);
      if (next.has(serial)) {
        next.delete(serial);
      } else {
        next.add(serial);
      }
      return next;
    });
  };

  const selectAll = () => {
    setSelectedSerials(new Set(streamingDevices.map(d => d.serial)));
  };

  const handleStart = useCallback(() => {
    if (selectedDevices.length === 0) return;

    const handler = new MultiplexerInputHandler();
    multiplexerRef.current = handler;
    setControlStatus({});
    // Ignore late events from a handler that has since been stopped.
    handler.setOnStatusChange((serial, status) => {
      if (multiplexerRef.current !== handler) return;
      setControlStatus(prev => ({ ...prev, [serial]: status }));
    });
    handler.setDevices(selectedDevices.map(d => d.serial));

    setActive(true);
  }, [selectedDevices]);

  // Keep the broadcast set identical to the tiles on screen. When a device's
  // session ends, the poll drops it from selectedDevices (and the grid); it
  // must stop receiving input too, or keystrokes reach a device the user
  // can no longer see. Keyed on the serial list so polls don't re-run it.
  const selectedKey = selectedDevices.map(d => d.serial).join('\n');
  useEffect(() => {
    multiplexerRef.current?.setDevices(selectedKey ? selectedKey.split('\n') : []);
  }, [selectedKey]);

  const handleStop = useCallback(() => {
    if (multiplexerRef.current) {
      multiplexerRef.current.disconnect();
      multiplexerRef.current = null;
    }
    setPending(null);
    setControlStatus({});
    setActive(false);
  }, []);

  // Attach canvas when primary canvas ref changes
  const setPrimaryCanvas = useCallback((el: HTMLCanvasElement | null) => {
    primaryCanvasRef.current = el;
    if (el && multiplexerRef.current) {
      multiplexerRef.current.attachCanvas(el);
      el.tabIndex = 0;
      el.focus();
    }
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (multiplexerRef.current) {
        multiplexerRef.current.disconnect();
      }
    };
  }, []);

  // A press that reaches more than one phone asks first; one phone never does.
  const handleNav = useCallback(
    (action: NavAction) => {
      if (targets.length > 1) {
        setPending({ kind: 'nav', action, n: targets.length });
      } else {
        multiplexerRef.current?.nav(action);
      }
    },
    [targets.length],
  );

  const handleTextSend = useCallback(() => {
    if (!textValue.trim() || !multiplexerRef.current) return;
    if (targets.length > 1) {
      setPending({ kind: 'text', text: textValue, n: targets.length });
      return;
    }
    multiplexerRef.current.sendText(textValue);
    setTextValue('');
  }, [textValue, targets.length]);

  const confirmPending = async () => {
    if (!pending || !multiplexerRef.current) return;
    if (pending.kind === 'nav') {
      multiplexerRef.current.nav(pending.action);
    } else {
      multiplexerRef.current.sendText(pending.text);
      setTextValue('');
    }
  };

  const targetNames = targets.map(displayName);

  return (
    <div className="flex flex-col nav:h-full">
      <PageHeader
        title="Multiplexer"
        sub="One input, many phones. What you do on the first screen is sent to every phone shown."
        actions={
          <>
            {!active && (
              <>
                <Button size="sm" onClick={selectAll} disabled={streamingDevices.length === 0}>
                  Select all
                </Button>
                <Button variant="primary" size="sm" onClick={handleStart} disabled={selectedDevices.length === 0}>
                  Start with {selectedDevices.length}
                </Button>
              </>
            )}
            {active && (
              <Button
                size="sm"
                icon={<Keyboard size={14} />}
                aria-pressed={showTextInput}
                onClick={() => setShowTextInput(t => !t)}
              >
                Keyboard
              </Button>
            )}
            <Button size="sm" icon={<RefreshCw size={14} />} onClick={fetchDevices}>
              Refresh
            </Button>
          </>
        }
      />

      {connectionLost && (
        <Banner tone="danger" className="m-4 mb-0 sm:mx-6">
          Connection lost, retrying. Showing the last phones we saw.
        </Banner>
      )}

      {/* While control is on, this stays put so it is obvious where typing goes. */}
      {active && (
        <div className="sticky top-0 z-20 bg-vx-canvas px-4 pt-4 sm:px-6">
          <Banner
            tone={targets.length > 0 ? 'warn' : 'info'}
            actions={
              <Button variant="danger" size="sm" icon={<Square size={12} />} onClick={handleStop}>
                Stop
              </Button>
            }
          >
            {targets.length > 0 ? (
              <>
                <strong className="font-semibold">Typing goes to {phones(targets.length)}</strong>
                <span className="text-vx-fg-muted"> · {compactNames(targetNames)}</span>
              </>
            ) : (
              <>
                <strong className="font-semibold">No phone is taking input yet</strong>
                <span className="text-vx-fg-muted"> · Waiting for connections, or you have watch-only access.</span>
              </>
            )}
          </Banner>
        </div>
      )}

      <div className="flex flex-col gap-4 p-4 sm:p-6 nav:min-h-0 nav:flex-1 nav:overflow-y-auto">
        {loading ? (
          <p className="text-sm text-vx-fg-muted">Loading phones…</p>
        ) : !active ? (
          // Phone selector
          streamingDevices.length === 0 ? (
            <EmptyState
              icon={<Keyboard />}
              title="No phones are streaming"
              action={<LinkButton href="/dashboard">Go to Devices</LinkButton>}
            >
              The multiplexer types into phones that have a session. Start one on the Devices page, then come back.
            </EmptyState>
          ) : (
            <div className="flex flex-col gap-3">
              <p className="max-w-prose text-sm text-vx-fg-muted">
                Pick the phones to control. The first one picked in the list becomes the input screen; everything you do
                there is sent to all of them.
              </p>
              <div className="grid grid-cols-1 gap-px border border-vx-hairline bg-vx-hairline sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
                {streamingDevices.map(device => {
                  const selected = selectedSerials.has(device.serial);
                  return (
                    <button
                      key={device.serial}
                      type="button"
                      aria-pressed={selected}
                      onClick={() => toggleDevice(device.serial)}
                      className={cx(
                        'flex min-h-touch items-center gap-3 px-3 py-2 text-left transition-colors',
                        selected ? 'bg-vx-accent/10 text-vx-fg' : 'bg-vx-surface text-vx-fg-muted hover:bg-vx-elevated',
                      )}
                    >
                      <span
                        aria-hidden
                        className={cx(
                          'flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border',
                          selected ? 'border-vx-accent bg-vx-accent text-vx-accent-ink' : 'border-vx-control',
                        )}
                      >
                        {selected && <Check size={12} strokeWidth={3} />}
                      </span>
                      <span className="min-w-0">
                        <span className="block truncate text-[13px] font-semibold text-vx-fg">{displayName(device)}</span>
                        <span className="block truncate font-mono text-[11px] tracking-wide text-vx-fg-label">
                          {device.serial}
                        </span>
                      </span>
                      <span className="sr-only">{selected ? 'Selected' : 'Not selected'}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          )
        ) : selectedDevices.length === 0 ? (
          <EmptyState icon={<Keyboard />} title="The selected phones stopped streaming">
            Their sessions ended. Stop here and pick phones again.
          </EmptyState>
        ) : (
          // Active multiplexer grid
          <div className="nav:min-h-0 nav:flex-1">
            <MultiplexerGrid
              devices={selectedDevices}
              primaryCanvasRef={setPrimaryCanvas}
              controlStatus={controlStatus}
            />
          </div>
        )}
      </div>

      {/* Navigation buttons, broadcast to every device */}
      {active && <NavBar onNav={handleNav} />}

      {/* Text input bar */}
      {active && showTextInput && (
        <form
          className="flex flex-col gap-2 border-t border-vx-hairline bg-vx-surface px-4 py-3 sm:flex-row sm:items-end"
          onSubmit={e => {
            e.preventDefault();
            handleTextSend();
          }}
        >
          <Field label="Text for every phone" className="flex min-w-0 flex-1 flex-col gap-2">
            <input
              type="text"
              value={textValue}
              onChange={e => setTextValue(e.target.value)}
              onKeyDown={e => e.stopPropagation()}
              placeholder="Type text to send to all phones"
              className="vx-input"
              autoComplete="off"
              autoFocus
            />
          </Field>
          <Button type="submit" variant="primary" size="lg" disabled={!textValue.trim()}>
            Send to all
          </Button>
        </form>
      )}

      <ConfirmDialog
        open={pending !== null}
        onClose={() => setPending(null)}
        onConfirm={confirmPending}
        tone="primary"
        title={
          pending
            ? pending.kind === 'nav'
              ? `Send ${NAV_LABEL[pending.action]} to ${phones(pending.n)}?`
              : `Send this text to ${phones(pending.n)}?`
            : ''
        }
        confirmLabel={pending ? `Send to ${pending.n}` : 'Send'}
        consequence={
          pending?.kind === 'text' ? (
            <>
              <span className="block break-words border-l-2 border-vx-control bg-vx-recessed px-3 py-2 font-mono text-[13px] text-vx-fg">
                {pending.text}
              </span>
              <span className="mt-2 block">It is typed into whatever is focused on {compactNames(targetNames)}.</span>
            </>
          ) : pending ? (
            <>
              {NAV_LABEL[pending.action]} is pressed on {compactNames(targetNames)}.
            </>
          ) : null
        }
      />
    </div>
  );
}
