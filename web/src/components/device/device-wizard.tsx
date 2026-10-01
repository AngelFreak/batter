'use client';

import { useState, useEffect } from 'react';
import {
  discoverDevices,
  validateDevice,
  probeDevice,
  registerDevice,
  listGroups,
  addDevicesToGroup,
  listUsers,
  grantAccess,
  DeviceInfo,
  DeviceGroup,
  UserInfo,
} from '@/lib/api';
import { isAdmin } from '@/lib/auth';
import { ScreenLockPanel } from './screen-lock-panel';
import { EthernetPanel } from './ethernet-panel';

interface DeviceWizardProps {
  onClose: () => void;
  onComplete: () => void;
}

type Step = 'serial' | 'validate' | 'details' | 'prepare' | 'ethernet' | 'groups' | 'access';

const STEPS: Step[] = ['serial', 'validate', 'details', 'prepare', 'ethernet', 'groups', 'access'];

const STEP_LABELS: Record<Step, string> = {
  serial: 'Serial',
  validate: 'Validate',
  details: 'Details',
  prepare: 'Prepare',
  ethernet: 'Ethernet',
  groups: 'Groups',
  access: 'Access',
};

export function DeviceWizard({ onClose, onComplete }: DeviceWizardProps) {
  const [step, setStep] = useState<Step>('serial');
  const [serial, setSerial] = useState('');
  const [nickname, setNickname] = useState('');
  const [validating, setValidating] = useState(false);
  const [validationState, setValidationState] = useState<string | null>(null);
  const [validationError, setValidationError] = useState('');
  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<{
    model: string;
    product: string;
    android_version: string;
  } | null>(null);
  const [registering, setRegistering] = useState(false);
  const [discoveredDevices, setDiscoveredDevices] = useState<DeviceInfo[]>([]);
  const [loadingSerials, setLoadingSerials] = useState(false);
  const [setupOpen, setSetupOpen] = useState(true);

  // Groups step
  const [groups, setGroups] = useState<DeviceGroup[]>([]);
  const [selectedGroups, setSelectedGroups] = useState<Set<string>>(new Set());
  const [savingGroups, setSavingGroups] = useState(false);

  // Access step
  const [users, setUsers] = useState<UserInfo[]>([]);
  const [userPermissions, setUserPermissions] = useState<Record<string, string>>({});
  const [savingAccess, setSavingAccess] = useState(false);

  const admin = isAdmin();
  const stepsToShow = admin ? STEPS : STEPS.filter((s) => s !== 'access');
  const currentIndex = stepsToShow.indexOf(step);

  // Discover ADB-visible devices for the serial picker
  const detectFromADB = async () => {
    setLoadingSerials(true);
    try {
      const devices = await discoverDevices();
      setDiscoveredDevices(devices);
    } catch {
      // ignore
    } finally {
      setLoadingSerials(false);
    }
  };

  // Auto-scan when entering the serial step
  useEffect(() => {
    if (step === 'serial') {
      detectFromADB();
    }
  }, []);

  // Auto-validate when entering validate step, auto-retry every 3s for non-ready states
  useEffect(() => {
    if (step === 'validate' && serial) {
      runValidation();

      const interval = setInterval(() => {
        // Keep retrying while on this step, unless already authorized
        setValidationState((current) => {
          if (current !== 'device') {
            runValidation();
          }
          return current;
        });
      }, 3000);

      return () => clearInterval(interval);
    }
  }, [step]);

  // Register device (bare) then probe when entering details step
  useEffect(() => {
    if (step === 'details' && serial && !probeResult) {
      (async () => {
        // Ensure device exists in DB before probing so probe can update it
        try { await registerDevice(serial); } catch { /* may already exist */ }
        runProbe();
      })();
    }
  }, [step]);

  // Load groups when entering groups step
  useEffect(() => {
    if (step === 'groups') {
      listGroups().then(setGroups).catch(() => {});
    }
  }, [step]);

  // Load users when entering access step
  useEffect(() => {
    if (step === 'access') {
      listUsers().then(setUsers).catch(() => {});
    }
  }, [step]);

  const runValidation = async () => {
    setValidating(true);
    setValidationError('');
    setValidationState(null);
    try {
      const result = await validateDevice(serial);
      if (result.reachable) {
        setValidationState(result.state);
      } else {
        setValidationError(result.error || 'Device not reachable');
      }
    } catch (err) {
      setValidationError(err instanceof Error ? err.message : 'Validation failed');
    } finally {
      setValidating(false);
    }
  };

  const runProbe = async () => {
    setProbing(true);
    try {
      const result = await probeDevice(serial);
      setProbeResult(result);
    } catch {
      // Allow continuing without probe data
      setProbeResult({ model: '', product: '', android_version: '' });
    } finally {
      setProbing(false);
    }
  };

  const handleRegisterAndNext = async () => {
    setRegistering(true);
    try {
      await registerDevice(serial, nickname);
      goNext();
    } catch (err) {
      // If it fails, still try to proceed (device may already be registered)
      goNext();
    } finally {
      setRegistering(false);
    }
  };

  const handleSaveGroups = async () => {
    setSavingGroups(true);
    try {
      for (const groupId of selectedGroups) {
        await addDevicesToGroup(groupId, [serial]);
      }
    } catch {
      // ignore individual failures
    } finally {
      setSavingGroups(false);
      goNext();
    }
  };

  const handleSaveAccess = async () => {
    setSavingAccess(true);
    try {
      for (const [userId, permission] of Object.entries(userPermissions)) {
        if (permission) {
          await grantAccess(userId, { device_serial: serial, permission });
        }
      }
    } catch {
      // ignore individual failures
    } finally {
      setSavingAccess(false);
      onComplete();
    }
  };

  const goNext = () => {
    const nextIdx = currentIndex + 1;
    if (nextIdx < stepsToShow.length) {
      setStep(stepsToShow[nextIdx]);
    } else {
      onComplete();
    }
  };

  const goBack = () => {
    const prevIdx = currentIndex - 1;
    if (prevIdx >= 0) {
      setStep(stepsToShow[prevIdx]);
    }
  };

  const canGoNext = () => {
    switch (step) {
      case 'serial':
        return serial.trim().length > 0;
      case 'validate':
        return validationState === 'device';
      case 'details':
        return !probing;
      default:
        return true;
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="bg-gray-900 border border-gray-800 rounded-xl w-full max-w-lg mx-4 shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-white">Add Device</h3>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-gray-300 text-lg leading-none"
          >
            &times;
          </button>
        </div>

        {/* Step indicator */}
        <div className="flex items-center gap-1 px-5 py-3 border-b border-gray-800">
          {stepsToShow.map((s, i) => (
            <div key={s} className="flex items-center gap-1">
              <div
                className={`w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-medium ${
                  i < currentIndex
                    ? 'bg-green-600 text-white'
                    : i === currentIndex
                    ? 'bg-brand-600 text-white'
                    : 'bg-gray-800 text-gray-500'
                }`}
              >
                {i < currentIndex ? '\u2713' : i + 1}
              </div>
              <span
                className={`text-[10px] ${
                  i === currentIndex ? 'text-gray-200' : 'text-gray-500'
                }`}
              >
                {STEP_LABELS[s]}
              </span>
              {i < stepsToShow.length - 1 && (
                <div className="w-4 h-px bg-gray-700 mx-1" />
              )}
            </div>
          ))}
        </div>

        {/* Content */}
        <div className="px-5 py-4 min-h-[200px]">
          {step === 'serial' && (
            <div className="space-y-4">
              {/* Setup instructions */}
              <div className="border border-gray-700 rounded-lg overflow-hidden">
                <button
                  onClick={() => setSetupOpen(!setupOpen)}
                  className="w-full flex items-center justify-between px-3 py-2 text-xs text-gray-300 hover:bg-gray-800/50"
                >
                  <span className="font-medium">Phone setup instructions</span>
                  <span className="text-gray-500 text-[10px]">{setupOpen ? '▲ Hide' : '▼ Show'}</span>
                </button>
                {setupOpen && (
                  <div className="px-3 pb-3 space-y-1.5">
                    <ol className="list-decimal list-inside text-[11px] text-gray-400 space-y-1.5">
                      <li>Connect the phone to this computer via USB</li>
                      <li>On the phone, go to <span className="text-gray-300">Settings &gt; About phone</span> and tap <span className="text-gray-300">Build number</span> 7 times to enable Developer Options</li>
                      <li>Go to <span className="text-gray-300">Settings &gt; System &gt; Developer options</span> and enable <span className="text-gray-300">USB debugging</span></li>
                      <li>If prompted on the phone, tap <span className="text-gray-300">Allow</span> on the &quot;Allow USB debugging?&quot; dialog</li>
                      <li>Click <span className="text-gray-300">Scan for devices</span> below</li>
                    </ol>
                  </div>
                )}
              </div>

              {/* Scan button and detected devices */}
              <div>
                <button
                  onClick={detectFromADB}
                  disabled={loadingSerials}
                  className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg border border-gray-700 flex items-center gap-2"
                >
                  {loadingSerials ? (
                    <>
                      <div className="w-3 h-3 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
                      Scanning...
                    </>
                  ) : (
                    'Scan for devices'
                  )}
                </button>
                {discoveredDevices.length > 0 && (
                  <div className="flex flex-wrap gap-1.5 mt-2">
                    {discoveredDevices.map((d) => (
                      <button
                        key={d.serial}
                        onClick={() => setSerial(d.serial)}
                        className={`px-2 py-1 rounded text-[10px] border transition-colors flex items-center gap-1.5 ${
                          serial === d.serial
                            ? 'bg-brand-600/20 border-brand-500 text-brand-300'
                            : 'bg-gray-800 border-gray-700 text-gray-400 hover:border-gray-600'
                        }`}
                      >
                        <div className={`w-1.5 h-1.5 rounded-full ${
                          d.state === 'device' ? 'bg-green-500' :
                          d.state === 'unauthorized' ? 'bg-yellow-500' :
                          'bg-red-500'
                        }`} />
                        <span className="font-mono">{d.serial}</span>
                        <span className={`${
                          d.state === 'device' ? 'text-green-400' :
                          d.state === 'unauthorized' ? 'text-yellow-400' :
                          'text-red-400'
                        }`}>
                          {d.state === 'device' ? '(ready)' :
                           d.state === 'unauthorized' ? '(unauthorized)' :
                           '(offline)'}
                        </span>
                      </button>
                    ))}
                  </div>
                )}
                {!loadingSerials && discoveredDevices.length === 0 && (
                  <div className="text-[10px] text-gray-500 mt-2">
                    No devices detected. Follow the setup instructions above and try again.
                  </div>
                )}
              </div>

              {/* Manual serial input */}
              <div>
                <label className="block text-xs text-gray-400 mb-1.5">
                  Or enter serial manually
                </label>
                <input
                  type="text"
                  value={serial}
                  onChange={(e) => setSerial(e.target.value)}
                  placeholder="e.g. ABCD1234"
                  className="w-full px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-200 placeholder-gray-600 focus:outline-none focus:border-brand-500"
                />
              </div>
            </div>
          )}

          {step === 'validate' && (
            <div className="space-y-4">
              <div className="text-xs text-gray-400">
                Validating connection to <span className="text-gray-200 font-mono">{serial}</span>...
              </div>
              {validating && (
                <div className="flex items-center gap-2">
                  <div className="w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
                  <span className="text-xs text-gray-400">Checking ADB state...</span>
                </div>
              )}
              {validationState === 'device' && (
                <div className="flex items-center gap-2 text-green-400 text-xs">
                  <div className="w-2 h-2 rounded-full bg-green-500" />
                  Device is connected and authorized
                </div>
              )}
              {validationState === 'unauthorized' && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2 text-yellow-400 text-xs">
                    <div className="w-2 h-2 rounded-full bg-yellow-500" />
                    Device is unauthorized
                  </div>
                  <div className="text-[11px] text-gray-400 bg-gray-800 rounded p-3 space-y-1">
                    <p>On your phone, look for the <span className="text-gray-200">&quot;Allow USB debugging?&quot;</span> popup.</p>
                    <p>Check <span className="text-gray-200">&quot;Always allow from this computer&quot;</span>, then tap <span className="text-gray-200">&quot;Allow&quot;</span>.</p>
                    <p className="text-gray-500">Retrying automatically...</p>
                  </div>
                </div>
              )}
              {validationState === 'offline' && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2 text-red-400 text-xs">
                    <div className="w-2 h-2 rounded-full bg-red-500" />
                    Device is offline
                  </div>
                  <div className="text-[11px] text-gray-400 bg-gray-800 rounded p-3 space-y-1">
                    <p>Try disconnecting and reconnecting the USB cable.</p>
                    <p className="text-gray-500">Retrying automatically...</p>
                  </div>
                </div>
              )}
              {validationError && (
                <div className="space-y-2">
                  <div className="text-red-400 text-xs">{validationError}</div>
                  <div className="text-[10px] text-gray-500">Retrying automatically...</div>
                </div>
              )}
            </div>
          )}

          {step === 'details' && (
            <div className="space-y-4">
              {probing ? (
                <div className="flex items-center gap-2">
                  <div className="w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
                  <span className="text-xs text-gray-400">Reading device properties...</span>
                </div>
              ) : (
                <>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block text-[10px] text-gray-500 mb-1">Model</label>
                      <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5">
                        {probeResult?.model || 'Unknown'}
                      </div>
                    </div>
                    <div>
                      <label className="block text-[10px] text-gray-500 mb-1">Product</label>
                      <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5">
                        {probeResult?.product || 'Unknown'}
                      </div>
                    </div>
                    <div>
                      <label className="block text-[10px] text-gray-500 mb-1">Android Version</label>
                      <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5">
                        {probeResult?.android_version || 'Unknown'}
                      </div>
                    </div>
                    <div>
                      <label className="block text-[10px] text-gray-500 mb-1">Serial</label>
                      <div className="text-xs text-gray-300 bg-gray-800 rounded px-2.5 py-1.5 font-mono">
                        {serial}
                      </div>
                    </div>
                  </div>
                  <div>
                    <label className="block text-xs text-gray-400 mb-1.5">
                      Nickname (optional)
                    </label>
                    <input
                      type="text"
                      value={nickname}
                      onChange={(e) => setNickname(e.target.value)}
                      placeholder="e.g. Test Phone 1"
                      className="w-full px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-200 placeholder-gray-600 focus:outline-none focus:border-brand-500"
                    />
                  </div>
                </>
              )}
            </div>
          )}

          {step === 'prepare' && <ScreenLockPanel serial={serial} />}

          {step === 'ethernet' && <EthernetPanel serial={serial} />}

          {step === 'groups' && (
            <div className="space-y-3">
              <div className="text-xs text-gray-400">
                Add this device to groups (optional)
              </div>
              {groups.length === 0 ? (
                <div className="text-xs text-gray-500">No groups created yet.</div>
              ) : (
                <div className="space-y-1.5 max-h-48 overflow-y-auto">
                  {groups.map((g) => (
                    <label
                      key={g.id}
                      className="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-gray-800 hover:bg-gray-750 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={selectedGroups.has(g.id)}
                        onChange={(e) => {
                          const next = new Set(selectedGroups);
                          if (e.target.checked) {
                            next.add(g.id);
                          } else {
                            next.delete(g.id);
                          }
                          setSelectedGroups(next);
                        }}
                        className="rounded border-gray-600"
                      />
                      <div className="flex items-center gap-2">
                        <div
                          className="w-2.5 h-2.5 rounded-full"
                          style={{ backgroundColor: g.color }}
                        />
                        <span className="text-xs text-gray-200">{g.name}</span>
                      </div>
                    </label>
                  ))}
                </div>
              )}
            </div>
          )}

          {step === 'access' && (
            <div className="space-y-3">
              <div className="text-xs text-gray-400">
                Grant user access to this device (optional)
              </div>
              {users.length === 0 ? (
                <div className="text-xs text-gray-500">No users found.</div>
              ) : (
                <div className="space-y-1.5 max-h-48 overflow-y-auto">
                  {users
                    .filter((u) => u.role !== 'admin')
                    .map((u) => (
                      <div
                        key={u.id}
                        className="flex items-center justify-between px-3 py-2 rounded-lg bg-gray-800"
                      >
                        <div>
                          <span className="text-xs text-gray-200">{u.display_name || u.username}</span>
                          <span className="text-[10px] text-gray-500 ml-2 capitalize">{u.role}</span>
                        </div>
                        <select
                          value={userPermissions[u.id] || ''}
                          onChange={(e) =>
                            setUserPermissions({ ...userPermissions, [u.id]: e.target.value })
                          }
                          className="px-2 py-1 text-[10px] bg-gray-700 border border-gray-600 rounded text-gray-300 focus:outline-none"
                        >
                          <option value="">No access</option>
                          <option value="view">View</option>
                          <option value="control">Control</option>
                          <option value="manage">Manage</option>
                        </select>
                      </div>
                    ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-5 py-3 border-t border-gray-800">
          <button
            onClick={currentIndex === 0 ? onClose : goBack}
            className="px-3 py-2 text-xs text-gray-400 hover:text-gray-200"
          >
            {currentIndex === 0 ? 'Cancel' : 'Back'}
          </button>
          <div className="flex items-center gap-2">
            {(step === 'prepare' || step === 'ethernet' || step === 'groups') && (
              <button
                onClick={goNext}
                className="px-3 py-2 text-xs text-gray-400 hover:text-gray-200"
              >
                Skip
              </button>
            )}
            {step === 'access' && (
              <button
                onClick={onComplete}
                className="px-3 py-2 text-xs text-gray-400 hover:text-gray-200"
              >
                Skip
              </button>
            )}
            <button
              onClick={() => {
                if (step === 'details') {
                  handleRegisterAndNext();
                } else if (step === 'groups') {
                  handleSaveGroups();
                } else if (step === 'access') {
                  handleSaveAccess();
                } else {
                  goNext();
                }
              }}
              disabled={!canGoNext() || registering || savingGroups || savingAccess}
              className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {step === 'details' && registering
                ? 'Registering...'
                : step === 'groups' && savingGroups
                ? 'Saving...'
                : step === 'access' && savingAccess
                ? 'Saving...'
                : currentIndex === stepsToShow.length - 1
                ? 'Done'
                : 'Next'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
