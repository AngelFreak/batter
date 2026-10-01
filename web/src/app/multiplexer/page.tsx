'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import { useRouter } from 'next/navigation';
import { listDevices, DeviceInfo } from '@/lib/api';
import { getToken } from '@/lib/auth';
import { MultiplexerInputHandler } from '@/lib/device-input-multiplexer';
import { MultiplexerGrid } from '@/components/device/multiplexer-grid';

export default function MultiplexerPage() {
  const router = useRouter();
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedSerials, setSelectedSerials] = useState<Set<string>>(new Set());
  const [active, setActive] = useState(false);
  const [textValue, setTextValue] = useState('');
  const [showTextInput, setShowTextInput] = useState(false);
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
    } catch {
      // ignore
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

  const handleTextSend = useCallback(() => {
    if (textValue.trim() && multiplexerRef.current) {
      multiplexerRef.current.sendText(textValue);
      setTextValue('');
    }
  }, [textValue]);

  return (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
        <div>
          <h2 className="text-lg font-semibold text-white">Multiplexer</h2>
          <p className="text-xs text-gray-500">
            {active
              ? `Broadcasting input to ${selectedDevices.length} device${selectedDevices.length !== 1 ? 's' : ''}`
              : `Select devices to control simultaneously`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {!active && (
            <>
              <button
                onClick={selectAll}
                disabled={streamingDevices.length === 0}
                className="px-2 py-1 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded disabled:opacity-50"
              >
                Select All
              </button>
              <button
                onClick={handleStart}
                disabled={selectedDevices.length === 0}
                className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50"
              >
                Start ({selectedDevices.length})
              </button>
            </>
          )}
          {active && (
            <>
              <button
                onClick={() => setShowTextInput(t => !t)}
                className={`px-2 py-1 text-[10px] rounded ${
                  showTextInput ? 'bg-brand-600 text-white' : 'bg-gray-800 hover:bg-gray-700 text-gray-300'
                }`}
              >
                Keyboard
              </button>
              <button
                onClick={handleStop}
                className="px-3 py-2 text-xs bg-red-600 hover:bg-red-700 text-white rounded-lg"
              >
                Stop
              </button>
            </>
          )}
          <button
            onClick={fetchDevices}
            className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg"
          >
            Refresh
          </button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 p-4 overflow-hidden min-h-0 flex flex-col gap-4">
        {loading ? (
          <div className="text-gray-500 text-sm">Loading devices...</div>
        ) : !active ? (
          // Device selector
          <div>
            {streamingDevices.length === 0 ? (
              <div className="text-gray-500 text-sm">No streaming devices. Start sessions from the Devices page.</div>
            ) : (
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2">
                {streamingDevices.map(device => (
                  <button
                    key={device.serial}
                    onClick={() => toggleDevice(device.serial)}
                    className={`flex items-center gap-2 px-3 py-2 rounded-lg border text-left transition-colors ${
                      selectedSerials.has(device.serial)
                        ? 'border-brand-500 bg-brand-600/10 text-brand-300'
                        : 'border-gray-800 bg-gray-900 text-gray-400 hover:border-gray-700'
                    }`}
                  >
                    <span className={`w-2 h-2 rounded-full ${
                      selectedSerials.has(device.serial) ? 'bg-brand-500' : 'bg-gray-600'
                    }`} />
                    <div className="min-w-0">
                      <div className="text-xs truncate">{device.nickname || device.model || device.serial}</div>
                      <div className="text-[10px] text-gray-600 truncate">{device.serial}</div>
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        ) : (
          // Active multiplexer grid
          <div className="flex-1 min-h-0">
            <MultiplexerGrid
              devices={selectedDevices}
              primaryCanvasRef={setPrimaryCanvas}
            />
          </div>
        )}
      </div>

      {/* Text input bar */}
      {active && showTextInput && (
        <div className="flex items-center gap-2 px-4 py-2 bg-gray-900 border-t border-gray-800">
          <input
            type="text"
            value={textValue}
            onChange={e => setTextValue(e.target.value)}
            onKeyDown={e => {
              e.stopPropagation();
              if (e.key === 'Enter') handleTextSend();
            }}
            placeholder="Type text to send to all devices..."
            className="flex-1 px-3 py-1.5 text-xs bg-gray-800 border border-gray-700 rounded text-gray-200 placeholder-gray-500 focus:outline-none focus:border-brand-500"
            autoFocus
          />
          <button
            onClick={handleTextSend}
            className="px-3 py-1.5 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded"
          >
            Send All
          </button>
        </div>
      )}
    </>
  );
}
