'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { LoaderCircle, Move, Home, PackagePlus, Power, RotateCw, Smartphone } from 'lucide-react';
import { DeviceVideoPlayer } from '@/lib/device-video';
import { DeviceInputHandler } from '@/lib/device-input';
import { DeviceAudioPlayer } from '@/lib/device-audio';
import { AudioControls, type AudioView } from '@/lib/audio-controls';
import { fetchScreenshot, pushFile, installAPK } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { audioReasonText } from '@/lib/server-text';
import { useNoSwipeNavigation } from '@/lib/use-no-swipe-navigation';
import { Button, StatusChip, useToast, type MenuItem } from '@/components/ui';
import { NavBar } from './nav-bar';
import { SwipePad } from './swipe-pad';
import { BackButton } from '@/components/viewer/back-button';
import { ViewerToolbar } from '@/components/viewer/viewer-toolbar';
import { ClipboardPanel, TextBar } from '@/components/viewer/viewer-panels';
import { isVideoFailure, videoView, type ViewerLive } from '@/components/viewer/viewer-status';
import { DEFAULT_VIEWER_PREFS, loadViewerPrefs, saveViewerPrefs, type ViewerPrefs } from '@/lib/viewer-prefs';

/** How long the video may take to start before the viewer offers Retry. */
const SLOW_START_MS = 15_000;

interface DeviceViewerProps {
  serial: string;
  /** Awaited before each video reconnect, e.g. to restart a lost session. */
  onStreamLost?: () => Promise<unknown>;
  /** Tells the page header how the stream is doing; null when the viewer goes away. */
  onLiveChange?: (live: ViewerLive | null) => void;
}

export function DeviceViewer({ serial, onStreamLost, onLiveChange }: DeviceViewerProps) {
  useNoSwipeNavigation();
  const toast = useToast();
  const toastRef = useRef(toast);
  toastRef.current = toast;
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<DeviceVideoPlayer | null>(null);
  const inputRef = useRef<DeviceInputHandler | null>(null);
  const audioRef = useRef<AudioControls | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const hadFrame = useRef(false);
  const [videoStatus, setVideoStatus] = useState('connecting');
  const [controlStatus, setControlStatus] = useState('connecting');
  const [fps, setFps] = useState(0);
  const [capturing, setCapturing] = useState(false);
  const [showFrame, setShowFrame] = useState(true);
  const [showTextInput, setShowTextInput] = useState(false);
  const [textValue, setTextValue] = useState('');
  const [clipboardText, setClipboardText] = useState('');
  const [dragging, setDragging] = useState(false);
  const [showClipboard, setShowClipboard] = useState(false);
  const [focused, setFocused] = useState(false);
  const [slow, setSlow] = useState(false);
  const [transfer, setTransfer] = useState<string | null>(null);
  const [audio, setAudio] = useState<AudioView>({ state: 'connecting', muted: true, volume: 1 });
  // Nav buttons and the swipe pad: this browser's choice, read after mount
  // (localStorage isn't there during server rendering).
  const [prefs, setPrefs] = useState<ViewerPrefs>(DEFAULT_VIEWER_PREFS);
  useEffect(() => setPrefs(loadViewerPrefs()), []);
  const updatePrefs = useCallback((change: Partial<ViewerPrefs>) => {
    setPrefs((p) => {
      const next = { ...p, ...change };
      saveViewerPrefs(next);
      return next;
    });
  }, []);
  // Read through a ref so a new callback identity doesn't recreate the player.
  const onStreamLostRef = useRef(onStreamLost);
  onStreamLostRef.current = onStreamLost;
  const onLiveChangeRef = useRef(onLiveChange);
  onLiveChangeRef.current = onLiveChange;

  const controlOk = controlStatus === 'connected';
  const controlReason =
    controlStatus === 'denied' ? "Your access doesn't include control" : 'Control is not connected yet';
  const keysToPhone = focused && controlOk;

  // Header: report what the stream is doing, and clear it when we go away.
  useEffect(() => {
    onLiveChangeRef.current?.({ video: videoStatus, control: controlStatus, fps, keysToPhone });
  }, [videoStatus, controlStatus, fps, keysToPhone]);
  useEffect(() => () => onLiveChangeRef.current?.(null), []);

  // Keep the last frame: once video has played, a drop is "reconnecting", not a blank start.
  useEffect(() => {
    if (videoStatus === 'streaming') hadFrame.current = true;
  }, [videoStatus]);

  // No video after a while: say so and offer Retry instead of an endless spinner.
  const waiting = videoStatus === 'connecting' || videoStatus === 'connected';
  useEffect(() => {
    if (!waiting || hadFrame.current) {
      setSlow(false);
      return;
    }
    const t = setTimeout(() => setSlow(true), SLOW_START_MS);
    return () => clearTimeout(t);
  }, [waiting]);

  useEffect(() => {
    if (!canvasRef.current) return;

    const canvas = canvasRef.current;

    // Video player
    const player = new DeviceVideoPlayer(canvas);
    playerRef.current = player;
    player.setOnStatusChange(setVideoStatus);
    player.setOnFpsUpdate(setFps);
    player.setBeforeReconnect(async () => onStreamLostRef.current?.());
    player.connect(serial);

    // Input handler
    const input = new DeviceInputHandler(canvas);
    inputRef.current = input;
    input.setOnStatusChange(setControlStatus);
    input.setOnClipboardReceive((text) => {
      setClipboardText(text);
      toastRef.current.success('Phone clipboard copied here and to your computer');
    });
    input.connect(serial);

    // Audio is independent of video and control: if it fails they carry on.
    const audioControls = new AudioControls(new DeviceAudioPlayer(), setAudio);
    audioRef.current = audioControls;
    audioControls.start(serial);

    // Keys reach the phone only while the screen has focus; the header says so.
    const onFocus = () => setFocused(true);
    const onBlur = () => setFocused(false);
    canvas.addEventListener('focus', onFocus);
    canvas.addEventListener('blur', onBlur);

    return () => {
      canvas.removeEventListener('focus', onFocus);
      canvas.removeEventListener('blur', onBlur);
      player.disconnect();
      input.disconnect();
      audioControls.stop();
      playerRef.current = null;
      inputRef.current = null;
      audioRef.current = null;
    };
  }, [serial]);

  const retry = useCallback(async () => {
    setSlow(false);
    try {
      await onStreamLostRef.current?.();
    } catch {
      // The reconnect below shows its own status if the session is still gone.
    }
    const player = playerRef.current;
    if (!player) return;
    player.disconnect();
    player.connect(serial);
  }, [serial]);

  const handleScreenshot = async () => {
    setCapturing(true);
    try {
      const blob = await fetchScreenshot(serial);
      if (!blob) {
        toast.error("Couldn't take a screenshot. Try again.");
        return;
      }
      const filename = `${serial}-${Date.now()}.png`;
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
      toast.success(`Screenshot saved as ${filename}`);
    } catch (err) {
      toast.error(errorMessage(err, 'take a screenshot'));
    } finally {
      setCapturing(false);
    }
  };

  const handleWake = () => {
    inputRef.current?.sendWake();
    toast.success('Wake sent');
  };

  const handleScreenOff = () => {
    inputRef.current?.sendScreenOff();
    toast.success('Screen off sent');
  };

  const handleTextSend = useCallback(() => {
    if (textValue.trim()) {
      inputRef.current?.sendText(textValue);
      setTextValue('');
    }
  }, [textValue]);

  // Paste to phone: the text in the panel, or your own clipboard if it is empty.
  const handlePasteToPhone = useCallback(async () => {
    let text = clipboardText;
    if (!text) {
      try {
        text = await navigator.clipboard.readText();
      } catch {
        toast.error("Couldn't read your clipboard. Allow clipboard access for this page, or type the text here.");
        return;
      }
      if (!text) {
        toast.info('Nothing to paste. Your clipboard is empty.');
        return;
      }
      setClipboardText(text);
    }
    inputRef.current?.sendSetClipboard(text, true);
    toast.success('Pasted to the phone');
  }, [clipboardText, toast]);

  const handleCopyFromPhone = useCallback(() => {
    inputRef.current?.sendGetClipboard();
  }, []);

  // File drop handlers
  const handleFileDrop = useCallback(
    async (files: FileList) => {
      if (!controlOk) {
        toast.error(`${controlReason}, so files can't be sent yet.`);
        return;
      }
      const list = Array.from(files);
      for (let i = 0; i < list.length; i++) {
        const file = list[i];
        const isAPK = file.name.toLowerCase().endsWith('.apk');
        const count = list.length > 1 ? ` (${i + 1} of ${list.length})` : '';
        setTransfer(`${isAPK ? 'Installing' : 'Pushing'} ${file.name}${count}`);
        try {
          if (isAPK) {
            await installAPK(serial, file);
            toast.success(`Installed ${file.name}`);
          } else {
            await pushFile(serial, file);
            toast.success(`Pushed ${file.name} to the phone`);
          }
        } catch (err) {
          toast.error(errorMessage(err, `${isAPK ? 'install' : 'push'} ${file.name}`));
        }
      }
      setTransfer(null);
    },
    [serial, toast, controlOk, controlReason],
  );

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragging(true);
  }, []);

  const onDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragging(false);
  }, []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setDragging(false);
      if (e.dataTransfer.files.length > 0) {
        handleFileDrop(e.dataTransfer.files);
      }
    },
    [handleFileDrop],
  );

  const handleFileInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (e.target.files && e.target.files.length > 0) {
        handleFileDrop(e.target.files);
        e.target.value = '';
      }
    },
    [handleFileDrop],
  );

  // "blocked" means the browser wants a click first, which the button is.
  const audioUsable = audio.state === 'available' || audio.state === 'blocked';
  const audioTitle =
    audio.state === 'unavailable'
      ? audioReasonText(audio.reason)
      : audio.state === 'unsupported'
        ? 'This browser cannot play device audio'
        : audio.state === 'needs-https'
          ? 'Device audio needs HTTPS'
          : audio.state === 'blocked'
            ? 'Click to allow sound'
            : audioUsable
              ? audio.muted
                ? 'Unmute device audio'
                : 'Mute device audio'
              : 'Connecting audio…';

  const moreItems: (MenuItem | 'separator')[] = [
    {
      label: 'Install APK',
      icon: <PackagePlus />,
      onSelect: () => fileInputRef.current?.click(),
      disabled: !controlOk,
      disabledReason: controlReason,
    },
    {
      label: 'Screen off',
      icon: <Power />,
      onSelect: handleScreenOff,
      disabled: !controlOk,
      disabledReason: controlReason,
    },
    'separator',
    {
      label: showFrame ? 'Hide device frame' : 'Show device frame',
      icon: <Smartphone />,
      onSelect: () => setShowFrame((f) => !f),
    },
    {
      label: prefs.navButtons ? 'Hide navigation buttons' : 'Show navigation buttons',
      icon: <Home />,
      onSelect: () => updatePrefs({ navButtons: !prefs.navButtons }),
    },
    {
      label: prefs.swipePad ? 'Hide swipe pad' : 'Show swipe pad',
      icon: <Move />,
      onSelect: () => updatePrefs({ swipePad: !prefs.swipePad }),
    },
  ];

  const failed = isVideoFailure(videoStatus);
  const starting = waiting && !hadFrame.current;
  const reconnecting = !failed && !starting && videoStatus !== 'streaming';
  const failView = videoView(videoStatus);

  return (
    <div className="flex min-h-[420px] flex-1 flex-col pb-[calc(3.5rem+env(safe-area-inset-bottom))] sm:pb-0">
      {/* Hidden file input for APK install */}
      <input ref={fileInputRef} type="file" accept=".apk" className="hidden" onChange={handleFileInputChange} />

      <ViewerToolbar
        audio={audio}
        audioUsable={audioUsable}
        audioTitle={audioTitle}
        onToggleMute={() => void audioRef.current?.toggleMute()}
        onVolume={(v) => audioRef.current?.setVolume(v)}
        keyboardOpen={showTextInput}
        onKeyboard={() => setShowTextInput((t) => !t)}
        clipboardOpen={showClipboard}
        onClipboard={() => setShowClipboard((c) => !c)}
        capturing={capturing}
        onScreenshot={handleScreenshot}
        onWake={handleWake}
        controlOk={controlOk}
        controlReason={controlReason}
        moreItems={moreItems}
      />

      {/* Narrow screens have no room for the volume slider in the bottom bar. */}
      {audioUsable && !audio.muted && (
        <label className="flex items-center gap-3 border-b border-vx-hairline bg-vx-surface px-4 py-2 sm:hidden">
          <span className="vx-label">Volume</span>
          <input
            type="range"
            min={0}
            max={1}
            step={0.05}
            value={audio.volume}
            onChange={(e) => audioRef.current?.setVolume(Number(e.target.value))}
            title={`Volume ${Math.round(audio.volume * 100)}%`}
            className="min-w-0 flex-1 accent-vx-accent"
          />
        </label>
      )}

      {/* Canvas with drop zone */}
      <div
        ref={containerRef}
        className="relative flex min-h-[240px] flex-1 items-center justify-center overflow-hidden bg-vx-recessed"
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onDrop={onDrop}
      >
        <canvas
          ref={canvasRef}
          tabIndex={0}
          role="application"
          aria-label="Phone screen. Click or focus to type into the phone; Shift+Escape returns to the page."
          className="max-h-full max-w-full object-contain"
          style={{
            cursor: 'default',
            ...(showFrame
              ? {
                  maxWidth: 'calc(100% - 32px)',
                  maxHeight: 'calc(100% - 32px)',
                  borderRadius: '16px',
                  boxShadow: '0 0 0 8px rgb(var(--vx-elevated)), 0 0 0 9px rgb(var(--vx-control))',
                }
              : {}),
          }}
        />

        {transfer && (
          <div
            role="status"
            className="absolute inset-x-0 top-0 z-10 border-b border-vx-hairline bg-vx-elevated px-3 py-2 text-sm text-vx-fg"
          >
            <div className="flex items-center gap-2">
              <LoaderCircle size={14} className="animate-spin text-vx-accent" aria-hidden />
              <span className="min-w-0 truncate">{transfer}…</span>
            </div>
            <div className="mt-2 h-0.5 bg-vx-hairline">
              <div className="h-full w-full animate-pulse bg-vx-accent" />
            </div>
          </div>
        )}

        {dragging && (
          <div className="absolute inset-0 z-20 flex items-center justify-center border-2 border-dashed border-vx-accent bg-vx-accent/10">
            <span className="font-mono text-xs uppercase tracking-wide text-vx-accent">Drop files to send to the phone</span>
          </div>
        )}

        {/* Reconnecting: the last frame stays visible underneath. */}
        {reconnecting && (
          <div className="absolute bottom-3 left-3 z-10 border border-vx-warn/40 bg-vx-elevated px-2.5 py-1.5">
            <StatusChip status={videoView('reconnecting')} />
          </div>
        )}

        {/* Starting for the first time. */}
        {starting && (
          <div role="status" className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-vx-recessed/90 p-4 text-center">
            {slow ? (
              <>
                <p className="text-lg font-extrabold">Still starting…</p>
                <p className="max-w-sm text-sm text-vx-fg-muted">
                  The video hasn&apos;t arrived yet. The phone may be busy or the session may need a restart.
                </p>
                <div className="flex flex-wrap justify-center gap-2">
                  <Button variant="primary" size="lg" icon={<RotateCw size={16} />} onClick={retry}>
                    Retry
                  </Button>
                  <BackButton />
                </div>
              </>
            ) : (
              <p className="inline-flex items-center gap-2 text-sm text-vx-fg-muted">
                <LoaderCircle size={16} className="animate-spin text-vx-accent" aria-hidden />
                Starting video…
              </p>
            )}
          </div>
        )}

        {/* The stream failed. */}
        {failed && (
          <div role="alert" className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 bg-vx-recessed/90 p-4 text-center">
            <StatusChip status={failView} withHint={false} />
            <p className="max-w-sm text-sm text-vx-fg-muted">{failView.hint}</p>
            <p className="font-mono text-[11px] uppercase tracking-wide text-vx-fg-label">Details: {videoStatus}</p>
            <Button variant="primary" size="lg" icon={<RotateCw size={16} />} onClick={retry}>
              Retry
            </Button>
          </div>
        )}

        {prefs.swipePad && (
          <SwipePad
            boundsRef={containerRef}
            pos={prefs.padPos}
            // While dragging only the position moves; it's saved when dropped.
            onMove={(padPos) => setPrefs((p) => ({ ...p, padPos }))}
            onMoved={(padPos) => updatePrefs({ padPos })}
            onSwipe={(dir) => inputRef.current?.swipe(dir)}
            disabled={!controlOk}
            disabledReason={controlReason}
          />
        )}
      </div>

      {prefs.navButtons && (
        <NavBar
          onNav={(action) => inputRef.current?.nav(action)}
          disabled={!controlOk}
          disabledReason={controlReason}
        />
      )}

      {showTextInput && (
        <TextBar
          value={textValue}
          onChange={setTextValue}
          onSend={handleTextSend}
          onClose={() => setShowTextInput(false)}
          disabled={!controlOk}
          reason={controlReason}
        />
      )}

      {showClipboard && (
        <ClipboardPanel
          value={clipboardText}
          onChange={setClipboardText}
          onPasteToPhone={handlePasteToPhone}
          onCopyFromPhone={handleCopyFromPhone}
          onClose={() => setShowClipboard(false)}
          disabled={!controlOk}
          reason={controlReason}
        />
      )}
    </div>
  );
}
