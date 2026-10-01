'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { DeviceVideoPlayer } from '@/lib/device-video';
import { DeviceInputHandler } from '@/lib/device-input';
import { DeviceAudioPlayer } from '@/lib/device-audio';
import { AudioControls, type AudioView } from '@/lib/audio-controls';
import { fetchScreenshot, pushFile, installAPK } from '@/lib/api';
import { useNoSwipeNavigation } from '@/lib/use-no-swipe-navigation';

interface DeviceViewerProps {
  serial: string;
  /** Awaited before each video reconnect, e.g. to restart a lost session. */
  onStreamLost?: () => Promise<unknown>;
}

export function DeviceViewer({ serial, onStreamLost }: DeviceViewerProps) {
  useNoSwipeNavigation();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<DeviceVideoPlayer | null>(null);
  const inputRef = useRef<DeviceInputHandler | null>(null);
  const audioRef = useRef<AudioControls | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [videoStatus, setVideoStatus] = useState('connecting');
  const [controlStatus, setControlStatus] = useState('connecting');
  const [fps, setFps] = useState(0);
  const [capturing, setCapturing] = useState(false);
  const [showFrame, setShowFrame] = useState(true);
  const [showTextInput, setShowTextInput] = useState(false);
  const [textValue, setTextValue] = useState('');
  const [toast, setToast] = useState<string | null>(null);
  const [clipboardText, setClipboardText] = useState('');
  const [dragging, setDragging] = useState(false);
  const [showClipboard, setShowClipboard] = useState(false);
  const [audio, setAudio] = useState<AudioView>({ state: 'connecting', muted: true, volume: 1 });
  // Read through a ref so a new callback identity doesn't recreate the player.
  const onStreamLostRef = useRef(onStreamLost);
  onStreamLostRef.current = onStreamLost;

  const showToast = useCallback((msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(null), 2000);
  }, []);

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
      showToast('Clipboard copied to host');
    });
    input.connect(serial);

    // Audio is independent of video and control: if it fails they carry on.
    const audioControls = new AudioControls(new DeviceAudioPlayer(), setAudio);
    audioRef.current = audioControls;
    audioControls.start(serial);

    // Make canvas focusable for keyboard events
    canvas.tabIndex = 0;
    canvas.focus();

    return () => {
      player.disconnect();
      input.disconnect();
      audioControls.stop();
      playerRef.current = null;
      inputRef.current = null;
      audioRef.current = null;
    };
  }, [serial, showToast]);

  const handleScreenshot = async () => {
    setCapturing(true);
    try {
      const blob = await fetchScreenshot(serial);
      if (blob) {
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${serial}-${Date.now()}.png`;
        a.click();
        URL.revokeObjectURL(url);
      }
    } finally {
      setCapturing(false);
    }
  };

  const handleTextSend = useCallback(() => {
    if (textValue.trim()) {
      inputRef.current?.sendText(textValue);
      setTextValue('');
    }
  }, [textValue]);

  const handleTextKeyDown = useCallback((e: React.KeyboardEvent<HTMLInputElement>) => {
    e.stopPropagation();
    if (e.key === 'Enter') {
      handleTextSend();
    }
  }, [handleTextSend]);

  const handlePaste = useCallback(async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) {
        inputRef.current?.sendSetClipboard(text, true);
        showToast('Pasted to device');
      }
    } catch {
      showToast('Clipboard access denied');
    }
  }, [showToast]);

  const handleCopy = useCallback(() => {
    inputRef.current?.sendGetClipboard();
  }, []);

  // File drop handlers
  const handleFileDrop = useCallback(async (files: FileList) => {
    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      const isAPK = file.name.toLowerCase().endsWith('.apk');
      try {
        if (isAPK) {
          showToast(`Installing ${file.name}...`);
          await installAPK(serial, file);
          showToast(`Installed ${file.name}`);
        } else {
          showToast(`Pushing ${file.name}...`);
          await pushFile(serial, file);
          showToast(`Pushed ${file.name}`);
        }
      } catch (err) {
        showToast(`Failed: ${err instanceof Error ? err.message : 'Unknown error'}`);
      }
    }
  }, [serial, showToast]);

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

  const onDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragging(false);
    if (e.dataTransfer.files.length > 0) {
      handleFileDrop(e.dataTransfer.files);
    }
  }, [handleFileDrop]);

  const handleInstallAPKClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      handleFileDrop(e.target.files);
      e.target.value = '';
    }
  }, [handleFileDrop]);

  // "blocked" means the browser wants a click first, which the button is.
  const audioUsable = audio.state === 'available' || audio.state === 'blocked';
  const audioTitle =
    audio.state === 'unavailable' ? `Audio unavailable${audio.reason ? `: ${audio.reason}` : ''}` :
    audio.state === 'unsupported' ? 'This browser cannot play device audio' :
    audio.state === 'needs-https' ? 'Device audio needs HTTPS' :
    audio.state === 'blocked' ? 'Click to allow sound' :
    audioUsable ? (audio.muted ? 'Unmute device audio' : 'Mute device audio') :
    'Connecting audio...';

  const statusColor = videoStatus === 'streaming' ? 'text-green-400' : 'text-yellow-400';

  return (
    <div className="flex flex-col h-full">
      {/* Hidden file input for APK install */}
      <input
        ref={fileInputRef}
        type="file"
        accept=".apk"
        className="hidden"
        onChange={handleFileInputChange}
      />

      {/* Status bar */}
      <div className="flex items-center justify-between px-4 py-2 bg-gray-900 border-b border-gray-800">
        <div className="flex items-center gap-3 text-xs">
          <span className={statusColor}>{videoStatus}</span>
          <span className="text-gray-500">|</span>
          <span className="text-gray-400">Control: {controlStatus}</span>
          {fps > 0 && (
            <>
              <span className="text-gray-500">|</span>
              <span className="text-gray-400">{fps} fps</span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => void audioRef.current?.toggleMute()}
            disabled={!audioUsable}
            title={audioTitle}
            aria-pressed={audioUsable && !audio.muted}
            className={`px-2 py-1 text-[10px] rounded disabled:opacity-50 ${
              audioUsable && !audio.muted ? 'bg-brand-600 text-white' : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
            }`}
          >
            {audio.state === 'connecting' || audio.state === 'reconnecting' ? 'Audio...' :
              !audioUsable ? 'No audio' : audio.muted ? 'Unmute' : 'Mute'}
          </button>
          {audioUsable && (
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={audio.volume}
              onChange={e => audioRef.current?.setVolume(Number(e.target.value))}
              aria-label="Volume"
              title={`Volume ${Math.round(audio.volume * 100)}%`}
              className="w-16 accent-brand-500"
            />
          )}
          <button
            onClick={() => setShowFrame(f => !f)}
            className={`px-2 py-1 text-[10px] rounded ${
              showFrame ? 'bg-brand-600 text-white' : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
            }`}
          >
            Frame
          </button>
          <button
            onClick={() => setShowTextInput(t => !t)}
            className={`px-2 py-1 text-[10px] rounded ${
              showTextInput ? 'bg-brand-600 text-white' : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
            }`}
          >
            Keyboard
          </button>
          <button
            onClick={() => setShowClipboard(c => !c)}
            className={`px-2 py-1 text-[10px] rounded ${
              showClipboard ? 'bg-brand-600 text-white' : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
            }`}
          >
            Clipboard
          </button>
          <button
            onClick={handlePaste}
            className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded"
          >
            Paste
          </button>
          <button
            onClick={handleCopy}
            className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded"
          >
            Copy
          </button>
          <button
            onClick={handleInstallAPKClick}
            className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded"
          >
            Install APK
          </button>
          <button
            onClick={handleScreenshot}
            disabled={capturing}
            className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded disabled:opacity-50"
          >
            {capturing ? 'Capturing...' : 'Screenshot'}
          </button>
          <button
            onClick={() => inputRef.current?.sendWake()}
            className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded"
          >
            Wake
          </button>
          <button
            onClick={() => inputRef.current?.sendScreenOff()}
            className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded"
          >
            Screen Off
          </button>
        </div>
      </div>

      {/* Canvas with drop zone */}
      <div
        ref={containerRef}
        className="flex-1 flex items-center justify-center bg-black overflow-hidden relative"
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onDrop={onDrop}
      >
        {dragging && (
          <div className="absolute inset-0 z-20 flex items-center justify-center bg-brand-600/20 border-2 border-dashed border-brand-500 rounded-lg">
            <span className="text-brand-300 text-sm font-medium">Drop files to push to device</span>
          </div>
        )}
        <canvas
          ref={canvasRef}
          className={showFrame ? 'object-contain' : 'max-w-full max-h-full object-contain'}
          style={{
            cursor: 'default',
            ...(showFrame ? {
              maxWidth: 'calc(100% - 32px)',
              maxHeight: 'calc(100% - 32px)',
              borderRadius: '2rem',
              boxShadow: '0 0 0 12px #1f2937, 0 0 0 13px #374151, 0 25px 50px -12px rgba(0,0,0,0.5)',
            } : {}),
          }}
        />
        {toast && (
          <div className="absolute bottom-4 left-1/2 -translate-x-1/2 px-3 py-1.5 bg-gray-800/90 text-gray-200 text-xs rounded-lg border border-gray-700 z-30">
            {toast}
          </div>
        )}
      </div>

      {/* Clipboard panel */}
      {showClipboard && (
        <div className="px-4 py-2 bg-gray-900 border-t border-gray-800">
          <div className="flex items-center gap-2 mb-1">
            <span className="text-[10px] text-gray-500 uppercase tracking-wider">Clipboard</span>
          </div>
          <div className="flex items-start gap-2">
            <textarea
              value={clipboardText}
              onChange={e => setClipboardText(e.target.value)}
              onKeyDown={e => e.stopPropagation()}
              rows={2}
              className="flex-1 px-3 py-1.5 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200 placeholder-gray-500 focus:outline-none focus:border-brand-500 resize-none"
              placeholder="Clipboard text..."
            />
            <div className="flex flex-col gap-1">
              <button
                onClick={handleCopy}
                className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded"
              >
                From Device
              </button>
              <button
                onClick={() => {
                  if (clipboardText) {
                    inputRef.current?.sendSetClipboard(clipboardText, true);
                    showToast('Pasted to device');
                  }
                }}
                className="px-2 py-1 text-[10px] bg-brand-600 hover:bg-brand-700 text-white rounded"
              >
                To Device
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Blind text input */}
      {showTextInput && (
        <div className="flex items-center gap-2 px-4 py-2 bg-gray-900 border-t border-gray-800">
          <input
            type="text"
            value={textValue}
            onChange={e => setTextValue(e.target.value)}
            onKeyDown={handleTextKeyDown}
            placeholder="Type text to send to device..."
            className="flex-1 px-3 py-1.5 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200 placeholder-gray-500 focus:outline-none focus:border-brand-500"
            autoFocus
          />
          <button
            onClick={handleTextSend}
            className="px-3 py-1.5 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded"
          >
            Send
          </button>
        </div>
      )}
    </div>
  );
}
