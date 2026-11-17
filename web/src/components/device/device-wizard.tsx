'use client';

import { useState, useEffect } from 'react';
import { ChevronDown, ChevronUp, RefreshCw } from 'lucide-react';
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
import { errorMessage } from '@/lib/api-error';
import type { StatusView } from '@/lib/device-status';
import { Button, Dialog, Field, FormError, StatusChip, cx, useToast } from '@/components/ui';
import { ScreenLockPanel } from './screen-lock-panel';
import { EthernetPanel } from './ethernet-panel';

interface DeviceWizardProps {
  open: boolean;
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

const FORM_ID = 'device-wizard-form';

/** What adb says about a phone, as a status the chip can show. */
function adbStatus(state: string): StatusView {
  if (state === 'device') return { label: 'Ready', tone: 'live', icon: 'dot', hint: '', code: state };
  if (state === 'unauthorized') {
    return { label: 'Needs approval', tone: 'warn', icon: 'triangle', hint: '', code: state };
  }
  return { label: 'Offline', tone: 'muted', icon: 'slash', hint: '', code: state };
}

function Callout({ tone = 'info', children }: { tone?: 'info' | 'warn'; children: React.ReactNode }) {
  return (
    <div
      className={cx(
        'space-y-1.5 border-l-2 px-3 py-2.5 text-sm text-vx-fg-muted',
        tone === 'warn' ? 'border-vx-warn bg-vx-warn/10' : 'border-vx-control bg-vx-elevated',
      )}
    >
      {children}
    </div>
  );
}

function Spinner({ label }: { label: string }) {
  return (
    <div role="status" className="flex items-center gap-2.5 text-sm text-vx-fg-muted">
      <RefreshCw size={16} className="animate-spin text-vx-accent" aria-hidden />
      {label}
    </div>
  );
}

function Fact({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="vx-label">{label}</dt>
      <dd className={cx('mt-1 break-words text-sm text-vx-fg', mono && 'font-mono text-[13px]')}>{value}</dd>
    </div>
  );
}

/**
 * Mount it fresh for each run (the dashboard keys it) so the steps start over.
 * It stays mounted while closed, so Dialog can hand focus back.
 */
export function DeviceWizard({ open, onClose, onComplete }: DeviceWizardProps) {
  const toast = useToast();
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
  const [probeFailed, setProbeFailed] = useState(false);
  const [registering, setRegistering] = useState(false);
  const [discoveredDevices, setDiscoveredDevices] = useState<DeviceInfo[]>([]);
  const [loadingSerials, setLoadingSerials] = useState(false);
  const [scanError, setScanError] = useState('');
  const [setupOpen, setSetupOpen] = useState(true);
  // What went wrong on the current step, if the user's action failed.
  const [stepError, setStepError] = useState('');

  // Groups step
  const [groups, setGroups] = useState<DeviceGroup[]>([]);
  const [groupsError, setGroupsError] = useState('');
  const [selectedGroups, setSelectedGroups] = useState<Set<string>>(new Set());
  const [savingGroups, setSavingGroups] = useState(false);

  // Access step
  const [users, setUsers] = useState<UserInfo[]>([]);
  const [usersError, setUsersError] = useState('');
  const [userPermissions, setUserPermissions] = useState<Record<string, string>>({});
  const [savingAccess, setSavingAccess] = useState(false);

  const admin = isAdmin();
  const stepsToShow = admin ? STEPS : STEPS.filter((s) => s !== 'access');
  const currentIndex = stepsToShow.indexOf(step);

  // Discover ADB-visible devices for the serial picker
  const detectFromADB = async () => {
    setLoadingSerials(true);
    setScanError('');
    try {
      const devices = await discoverDevices();
      setDiscoveredDevices(devices);
    } catch (err) {
      setScanError(errorMessage(err, 'scan for phones'));
    } finally {
      setLoadingSerials(false);
    }
  };

  // Auto-scan when entering the serial step
  useEffect(() => {
    if (step === 'serial') {
      detectFromADB();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // A new step starts without the last step's error.
  useEffect(() => {
    setStepError('');
  }, [step]);

  // Auto-validate when entering validate step, auto-retry every 3s for non-ready states
  useEffect(() => {
    if (open && step === 'validate' && serial) {
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, open]);

  // Register device (bare) then probe when entering details step
  useEffect(() => {
    if (step === 'details' && serial && !probeResult) {
      (async () => {
        // Ensure device exists in DB before probing so probe can update it
        try {
          await registerDevice(serial);
        } catch {
          /* may already exist */
        }
        runProbe();
      })();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step]);

  // Load groups when entering groups step
  useEffect(() => {
    if (step === 'groups') {
      setGroupsError('');
      listGroups()
        .then(setGroups)
        .catch((err) => setGroupsError(errorMessage(err, 'load the groups')));
    }
  }, [step]);

  // Load users when entering access step
  useEffect(() => {
    if (step === 'access') {
      setUsersError('');
      listUsers()
        .then(setUsers)
        .catch((err) => setUsersError(errorMessage(err, 'load the users')));
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
        setValidationError("Batter can't reach this phone yet. Check the cable and the serial.");
      }
    } catch (err) {
      setValidationError(errorMessage(err, 'check the phone'));
    } finally {
      setValidating(false);
    }
  };

  const runProbe = async () => {
    setProbing(true);
    setProbeFailed(false);
    try {
      const result = await probeDevice(serial);
      setProbeResult(result);
    } catch {
      // Allow continuing without probe data
      setProbeFailed(true);
      setProbeResult({ model: '', product: '', android_version: '' });
    } finally {
      setProbing(false);
    }
  };

  const finish = () => {
    toast.success('Phone added.');
    onComplete();
  };

  const handleRegisterAndNext = async () => {
    setRegistering(true);
    setStepError('');
    try {
      await registerDevice(serial, nickname);
      goNext();
    } catch (err) {
      setStepError(errorMessage(err, 'save the phone'));
    } finally {
      setRegistering(false);
    }
  };

  const handleSaveGroups = async () => {
    setSavingGroups(true);
    setStepError('');
    const failed: string[] = [];
    let firstError: unknown = null;
    for (const groupId of selectedGroups) {
      try {
        await addDevicesToGroup(groupId, [serial]);
      } catch (err) {
        firstError ??= err;
        failed.push(groups.find((g) => g.id === groupId)?.name ?? 'a group');
      }
    }
    setSavingGroups(false);
    if (failed.length > 0) {
      setStepError(`${errorMessage(firstError, 'add the phone to a group')} Not added to: ${failed.join(', ')}. Retry, or skip.`);
      return;
    }
    goNext();
  };

  const handleSaveAccess = async () => {
    setSavingAccess(true);
    setStepError('');
    const failed: string[] = [];
    let firstError: unknown = null;
    for (const [userId, permission] of Object.entries(userPermissions)) {
      if (!permission) continue;
      try {
        await grantAccess(userId, { device_serial: serial, permission });
      } catch (err) {
        firstError ??= err;
        const u = users.find((x) => x.id === userId);
        failed.push(u?.display_name || u?.username || 'a user');
      }
    }
    setSavingAccess(false);
    if (failed.length > 0) {
      setStepError(`${errorMessage(firstError, 'grant access')} No access for: ${failed.join(', ')}. Retry, or skip.`);
      return;
    }
    finish();
  };

  const goNext = () => {
    const nextIdx = currentIndex + 1;
    if (nextIdx < stepsToShow.length) {
      setStep(stepsToShow[nextIdx]);
    } else {
      finish();
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

  const working = registering || savingGroups || savingAccess;

  const primaryAction = () => {
    if (!canGoNext() || working) return;
    if (step === 'details') {
      handleRegisterAndNext();
    } else if (step === 'groups') {
      handleSaveGroups();
    } else if (step === 'access') {
      handleSaveAccess();
    } else {
      goNext();
    }
  };

  const nextLabel =
    step === 'details' && registering
      ? 'Registering…'
      : (step === 'groups' && savingGroups) || (step === 'access' && savingAccess)
      ? 'Saving…'
      : currentIndex === stepsToShow.length - 1
      ? 'Done'
      : 'Next';

  const stepCount = stepsToShow.length;

  return (
    <Dialog
      open={open}
      onClose={onClose}
      size="lg"
      busy={working}
      title="Add a phone"
      description={
        <span className="vx-label">
          Step {currentIndex + 1} of {stepCount} · {STEP_LABELS[step]}
        </span>
      }
      footer={
        <>
          <Button variant="quiet" className="mr-auto" onClick={currentIndex === 0 ? onClose : goBack} disabled={working}>
            {currentIndex === 0 ? 'Cancel' : 'Back'}
          </Button>
          {(step === 'prepare' || step === 'ethernet' || step === 'groups') && (
            <Button variant="quiet" onClick={goNext} disabled={working}>
              Skip
            </Button>
          )}
          {step === 'access' && (
            <Button variant="quiet" onClick={finish} disabled={working}>
              Skip
            </Button>
          )}
          <Button type="submit" form={FORM_ID} variant="primary" disabled={!canGoNext() || working}>
            {nextLabel}
          </Button>
        </>
      }
    >
      <form
        id={FORM_ID}
        onSubmit={(e) => {
          e.preventDefault();
          primaryAction();
        }}
        className="flex min-h-[200px] flex-col gap-5"
      >
        {/* Progress, as segments; the label above says the same in words */}
        <div className="flex gap-1" aria-hidden>
          {stepsToShow.map((s, i) => (
            <div key={s} className={cx('h-0.5 flex-1', i <= currentIndex ? 'bg-vx-accent' : 'bg-vx-hairline')} />
          ))}
        </div>

        {step === 'serial' && (
          <div className="flex flex-col gap-5">
            {/* Setup instructions */}
            <div className="border border-vx-hairline">
              <button
                type="button"
                onClick={() => setSetupOpen(!setupOpen)}
                aria-expanded={setupOpen}
                className="flex min-h-touch w-full items-center justify-between gap-3 px-3.5 text-left text-sm font-bold text-vx-fg hover:bg-vx-elevated"
              >
                <span>Phone setup instructions</span>
                {setupOpen ? <ChevronUp size={16} aria-hidden /> : <ChevronDown size={16} aria-hidden />}
              </button>
              {setupOpen && (
                <ol className="list-decimal space-y-2 border-t border-vx-hairline py-3 pl-9 pr-4 text-[13.5px] text-vx-fg-muted">
                  <li>Connect the phone to this computer by USB.</li>
                  <li>
                    On the phone, go to <span className="text-vx-fg">Settings &gt; About phone</span> and tap{' '}
                    <span className="text-vx-fg">Build number</span> 7 times to enable Developer options.
                  </li>
                  <li>
                    Go to <span className="text-vx-fg">Settings &gt; System &gt; Developer options</span> and enable{' '}
                    <span className="text-vx-fg">USB debugging</span>.
                  </li>
                  <li>
                    If the phone asks &quot;Allow USB debugging?&quot;, tap <span className="text-vx-fg">Allow</span>.
                  </li>
                  <li>
                    Select <span className="text-vx-fg">Scan for phones</span> below.
                  </li>
                </ol>
              )}
            </div>

            {/* Scan button and detected devices */}
            <div className="flex flex-col items-start gap-3">
              <Button
                onClick={detectFromADB}
                disabled={loadingSerials}
                icon={<RefreshCw size={14} className={loadingSerials ? 'animate-spin' : undefined} />}
              >
                {loadingSerials ? 'Scanning…' : 'Scan for phones'}
              </Button>
              <FormError>{scanError}</FormError>
              {discoveredDevices.length > 0 && (
                <ul className="flex flex-wrap gap-2" aria-label="Phones found">
                  {discoveredDevices.map((d) => (
                    <li key={d.serial}>
                      <button
                        type="button"
                        aria-pressed={serial === d.serial}
                        onClick={() => setSerial(d.serial)}
                        className={cx(
                          'flex min-h-touch items-center gap-3 rounded-sm border px-3 text-left transition-colors',
                          serial === d.serial
                            ? 'border-vx-accent bg-vx-accent/10'
                            : 'border-vx-control hover:border-vx-accent',
                        )}
                      >
                        <span className="font-mono text-[13px] text-vx-fg">{d.serial}</span>
                        <StatusChip status={adbStatus(d.state)} withHint={false} />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {!loadingSerials && !scanError && discoveredDevices.length === 0 && (
                <p className="text-[13px] text-vx-fg-muted">
                  No phones detected. Follow the setup instructions above and scan again.
                </p>
              )}
            </div>

            {/* Manual serial input */}
            <Field label="Or enter the serial" hint="Shown by the scan, or by `adb devices`.">
              <input
                type="text"
                name="serial"
                value={serial}
                onChange={(e) => setSerial(e.target.value)}
                placeholder="e.g. ABCD1234"
                className="vx-input font-mono"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </Field>
          </div>
        )}

        {step === 'validate' && (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-vx-fg-muted">
              Checking the connection to <span className="font-mono text-[13px] text-vx-fg">{serial}</span>.
            </p>
            {validating && <Spinner label="Checking adb…" />}
            {validationState === 'device' && <StatusChip status={adbStatus('device')} withHint={false} />}
            {validationState === 'unauthorized' && (
              <div className="flex flex-col gap-3">
                <StatusChip status={adbStatus('unauthorized')} withHint={false} />
                <Callout tone="warn">
                  <p>
                    On the phone, look for <span className="text-vx-fg">&quot;Allow USB debugging?&quot;</span>.
                  </p>
                  <p>
                    Tick <span className="text-vx-fg">&quot;Always allow from this computer&quot;</span>, then tap{' '}
                    <span className="text-vx-fg">Allow</span>.
                  </p>
                  <p>Retrying every few seconds.</p>
                </Callout>
              </div>
            )}
            {validationState === 'offline' && (
              <div className="flex flex-col gap-3">
                <StatusChip status={adbStatus('offline')} withHint={false} />
                <Callout>
                  <p>Unplug the USB cable and plug it in again.</p>
                  <p>Retrying every few seconds.</p>
                </Callout>
              </div>
            )}
            {validationError && (
              <div className="flex flex-col gap-2">
                <FormError>{validationError}</FormError>
                <p className="text-[13px] text-vx-fg-muted">Retrying every few seconds.</p>
              </div>
            )}
          </div>
        )}

        {step === 'details' && (
          <div className="flex flex-col gap-5">
            {probing ? (
              <Spinner label="Reading the phone's properties…" />
            ) : (
              <>
                <dl className="grid grid-cols-2 gap-x-4 gap-y-4">
                  <Fact label="Model" value={probeResult?.model || 'Unknown'} />
                  <Fact label="Product" value={probeResult?.product || 'Unknown'} />
                  <Fact label="Android version" value={probeResult?.android_version || 'Unknown'} />
                  <Fact label="Serial" value={serial} mono />
                </dl>
                {probeFailed && (
                  <Callout tone="warn">
                    <p>Couldn&apos;t read the phone&apos;s details. You can still add it.</p>
                  </Callout>
                )}
                <Field label="Nickname (optional)" hint="Shown instead of the model name.">
                  <input
                    type="text"
                    name="nickname"
                    value={nickname}
                    onChange={(e) => setNickname(e.target.value)}
                    placeholder="e.g. Test phone 1"
                    className="vx-input"
                    autoComplete="off"
                  />
                </Field>
              </>
            )}
          </div>
        )}

        {step === 'prepare' && <ScreenLockPanel serial={serial} />}

        {step === 'ethernet' && <EthernetPanel serial={serial} />}

        {step === 'groups' && (
          <fieldset className="flex flex-col gap-3">
            <legend className="mb-3 text-sm text-vx-fg-muted">Add this phone to groups. Optional.</legend>
            {groupsError ? (
              <FormError>{groupsError}</FormError>
            ) : groups.length === 0 ? (
              <p className="text-sm text-vx-fg-muted">No groups created yet.</p>
            ) : (
              <div className="flex max-h-60 flex-col gap-px overflow-y-auto border border-vx-hairline bg-vx-hairline">
                {groups.map((g) => (
                  <label
                    key={g.id}
                    className="flex min-h-touch cursor-pointer items-center gap-3 bg-vx-surface px-3.5 hover:bg-vx-elevated"
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
                      className="h-4 w-4 accent-vx-accent"
                    />
                    <span className="h-2.5 w-2.5 shrink-0" style={{ backgroundColor: g.color }} aria-hidden />
                    <span className="min-w-0 truncate text-sm text-vx-fg">{g.name}</span>
                  </label>
                ))}
              </div>
            )}
          </fieldset>
        )}

        {step === 'access' && (
          <div className="flex flex-col gap-3">
            <p className="text-sm text-vx-fg-muted">Give users access to this phone. Optional.</p>
            {usersError ? (
              <FormError>{usersError}</FormError>
            ) : users.length === 0 ? (
              <p className="text-sm text-vx-fg-muted">No users found.</p>
            ) : (
              <div className="flex max-h-60 flex-col gap-px overflow-y-auto border border-vx-hairline bg-vx-hairline">
                {users
                  .filter((u) => u.role !== 'admin')
                  .map((u) => {
                    const label = u.display_name || u.username;
                    return (
                      <div key={u.id} className="flex items-center justify-between gap-3 bg-vx-surface px-3.5 py-2">
                        <div className="min-w-0">
                          <span className="block truncate text-sm text-vx-fg">{label}</span>
                          <span className="vx-label">{u.role}</span>
                        </div>
                        <select
                          aria-label={`Access for ${label}`}
                          value={userPermissions[u.id] || ''}
                          onChange={(e) => setUserPermissions({ ...userPermissions, [u.id]: e.target.value })}
                          className="vx-input w-36 shrink-0"
                        >
                          <option value="">No access</option>
                          <option value="view">View</option>
                          <option value="control">Control</option>
                          <option value="manage">Manage</option>
                        </select>
                      </div>
                    );
                  })}
              </div>
            )}
          </div>
        )}

        <FormError>{stepError}</FormError>
      </form>
    </Dialog>
  );
}
