'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { getToken, isAdmin } from '@/lib/auth';
import {
  listVPNProfiles, createVPNProfile, updateVPNProfile, deleteVPNProfile, checkVPNProfileExitIP,
  type VPNProfile,
} from '@/lib/api';

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MiB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GiB`;
}

function formatHandshake(iso?: string): string {
  if (!iso) return 'never';
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.round(secs / 60)}m ago`;
  return new Date(iso).toLocaleString();
}

const inputClass =
  'w-full px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white placeholder-gray-600 focus:outline-none focus:border-brand-600';

const configPlaceholder =
  '[Interface]\nPrivateKey = ...\nAddress = 10.64.0.2/32\nDNS = 10.64.0.1\n\n[Peer]\nPublicKey = ...\nEndpoint = vpn.example.net:51820\nAllowedIPs = 0.0.0.0/0';

function Switch({ on, disabled, label, onChange }: { on: boolean; disabled: boolean; label: string; onChange: () => void }) {
  return (
    <button
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={onChange}
      disabled={disabled}
      className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-50 ${
        on ? 'bg-brand-600' : 'bg-gray-700'
      }`}
    >
      <span
        className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${on ? 'translate-x-4' : 'translate-x-0.5'}`}
      />
    </button>
  );
}

// ProfileForm creates a profile, or edits one (blank config keeps the saved one).
function ProfileForm({
  profile, busy, onSubmit, onCancel,
}: {
  profile?: VPNProfile;
  busy: boolean;
  onSubmit: (name: string, config: string) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(profile?.name ?? '');
  const [config, setConfig] = useState('');
  const canSave = name.trim() !== '' && (profile !== undefined || config.trim() !== '');
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-3">
      <input
        type="text"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Name, e.g. Sweden"
        className={inputClass}
        autoFocus
      />
      <textarea
        value={config}
        onChange={(e) => setConfig(e.target.value)}
        rows={10}
        spellCheck={false}
        placeholder={profile ? 'Paste a new WireGuard config to replace the saved one (optional)' : configPlaceholder}
        className={`${inputClass} font-mono`}
      />
      <p className="text-[10px] text-gray-500">
        A standard wg-quick .conf from your VPN provider. The private key is stored in Batter&apos;s
        database and is never shown again.
      </p>
      <div className="flex gap-2">
        <button
          onClick={() => onSubmit(name, config)}
          disabled={busy || !canSave}
          className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50"
        >
          {busy ? 'Saving...' : profile ? 'Save' : 'Add profile'}
        </button>
        <button onClick={onCancel} className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg">
          Cancel
        </button>
      </div>
    </div>
  );
}

export default function AdminVPNPage() {
  const router = useRouter();
  const [profiles, setProfiles] = useState<VPNProfile[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [exits, setExits] = useState<Record<string, { exit_ip?: string; error?: string }>>({});

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
  }, [router]);

  const refresh = useCallback(async () => {
    try {
      setProfiles(await listVPNProfiles());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load VPN profiles');
    }
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 5000);
    return () => clearInterval(t);
  }, [refresh]);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      await refresh();
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Request failed');
      return false;
    } finally {
      setBusy(false);
    }
  };

  const handleCheck = async (id: string) => {
    setExits((x) => ({ ...x, [id]: {} }));
    try {
      const res = await checkVPNProfileExitIP(id);
      setExits((x) => ({ ...x, [id]: res }));
    } catch (e) {
      setExits((x) => ({ ...x, [id]: { error: e instanceof Error ? e.message : 'Check failed' } }));
    }
  };

  return (
    <>
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
        <div>
          <h2 className="text-lg font-semibold text-white">VPN profiles</h2>
          <p className="text-xs text-gray-500">
            {profiles ? `${profiles.length} profile${profiles.length !== 1 ? 's' : ''}` : 'Loading...'}
          </p>
        </div>
        <button
          onClick={() => { setAdding(true); setEditing(null); }}
          className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg"
        >
          New Profile
        </button>
      </div>

      <div className="flex-1 overflow-auto">
        <div className="max-w-4xl mx-auto p-6 space-y-4">
          <p className="text-xs text-gray-400">
            Phones on the phone network (ethernet adapters) reach the internet through the VPN profile
            chosen for them, and only through it; DNS too. Nothing else uses these tunnels: Batter, adb and
            the host keep their normal route. When a profile is disabled or its tunnel is down, its phones
            have no internet at all rather than going out directly; other profiles are unaffected. Give each
            profile a <code>DNS =</code> line: phones&apos; DNS queries go to that server, through the tunnel.
          </p>

          {error && <div className="text-xs text-red-400">{error}</div>}

          {adding && (
            <ProfileForm
              busy={busy}
              onCancel={() => setAdding(false)}
              onSubmit={async (name, config) => {
                if (await run(() => createVPNProfile(name, config))) setAdding(false);
              }}
            />
          )}

          {profiles?.length === 0 && !adding && (
            <div className="text-xs text-gray-500">No profiles yet. Add one to give phones internet.</div>
          )}

          {profiles?.map((p) => {
            const exit = exits[p.id];
            if (editing === p.id) {
              return (
                <ProfileForm
                  key={p.id}
                  profile={p}
                  busy={busy}
                  onCancel={() => setEditing(null)}
                  onSubmit={async (name, config) => {
                    const changes = config.trim() ? { name, config } : { name };
                    if (await run(() => updateVPNProfile(p.id, changes))) setEditing(null);
                  }}
                />
              );
            }
            return (
              <div key={p.id} className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <div>
                    <div className="text-sm font-semibold text-white">{p.name}</div>
                    <div className="text-[10px] text-gray-500">
                      {p.devices} phone{p.devices !== 1 ? 's' : ''} ·{' '}
                      <span className={!p.enabled ? 'text-gray-400' : p.status?.up ? 'text-green-400' : 'text-yellow-400'}>
                        {!p.enabled ? 'Disabled (its phones have no internet)' : p.status?.up ? 'Tunnel up' : 'Tunnel down'}
                      </span>
                    </div>
                  </div>
                  <Switch
                    on={p.enabled}
                    disabled={busy}
                    label={`${p.name} enabled`}
                    onChange={() => run(() => updateVPNProfile(p.id, { enabled: !p.enabled }))}
                  />
                </div>

                {p.apply_error && (
                  <div className="text-xs text-yellow-400 bg-yellow-900/20 border border-yellow-800/50 rounded-lg p-3">
                    Saved, but not fully applied: {p.apply_error}
                  </div>
                )}

                <div className="grid grid-cols-2 gap-3 text-xs">
                  <div>
                    <div className="text-[10px] text-gray-500 mb-1">Address</div>
                    <div className="text-gray-300 font-mono">{p.config.addresses.join(', ')}</div>
                  </div>
                  <div>
                    <div className="text-[10px] text-gray-500 mb-1">DNS for phones</div>
                    <div className="text-gray-300 font-mono">{p.config.dns?.join(', ') || 'default (8.8.8.8)'}</div>
                  </div>
                  <div className="col-span-2">
                    <div className="text-[10px] text-gray-500 mb-1">This server&apos;s public key</div>
                    <div className="text-gray-300 font-mono break-all">{p.config.public_key}</div>
                  </div>
                </div>

                {p.config.peers.map((peer) => {
                  const live = p.status?.peers.find((s) => s.public_key === peer.public_key);
                  return (
                    <div key={peer.public_key} className="border-t border-gray-800 pt-3 grid grid-cols-2 gap-3 text-xs">
                      <div>
                        <div className="text-[10px] text-gray-500 mb-1">Endpoint</div>
                        <div className="text-gray-300 font-mono">{live?.endpoint || peer.endpoint}</div>
                      </div>
                      <div>
                        <div className="text-[10px] text-gray-500 mb-1">Latest handshake</div>
                        <div className="text-gray-300">{p.enabled ? formatHandshake(live?.latest_handshake) : '-'}</div>
                      </div>
                      <div>
                        <div className="text-[10px] text-gray-500 mb-1">Received / sent</div>
                        <div className="text-gray-300">
                          {live ? `${formatBytes(live.rx_bytes)} / ${formatBytes(live.tx_bytes)}` : '-'}
                        </div>
                      </div>
                      <div>
                        <div className="text-[10px] text-gray-500 mb-1">Allowed IPs</div>
                        <div className="text-gray-300 font-mono">{peer.allowed_ips.join(', ')}</div>
                      </div>
                    </div>
                  );
                })}

                <div className="border-t border-gray-800 pt-3 flex flex-wrap items-center gap-2">
                  <button
                    onClick={() => handleCheck(p.id)}
                    className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-200 rounded-lg"
                  >
                    Check exit IP
                  </button>
                  {exit && !exit.exit_ip && !exit.error && <span className="text-xs text-gray-500">Checking...</span>}
                  {exit?.exit_ip && (
                    <span className="text-xs text-gray-300">
                      Exits from <span className="font-mono text-white">{exit.exit_ip}</span>
                    </span>
                  )}
                  {exit?.error && <span className="text-xs text-red-400">No internet for its phones: {exit.error}</span>}
                  <span className="flex-1" />
                  <button onClick={() => { setEditing(p.id); setAdding(false); }} className="text-xs text-brand-400 hover:text-brand-300">
                    Edit
                  </button>
                  {confirmDelete !== p.id ? (
                    <button onClick={() => setConfirmDelete(p.id)} className="text-xs text-red-400 hover:text-red-300">
                      Delete
                    </button>
                  ) : (
                    <span className="flex items-center gap-2">
                      <span className="text-xs text-red-400">
                        {p.devices > 0 ? `${p.devices} phone${p.devices !== 1 ? 's' : ''} will have no internet.` : 'Delete?'}
                      </span>
                      <button
                        onClick={async () => { await run(() => deleteVPNProfile(p.id)); setConfirmDelete(null); }}
                        disabled={busy}
                        className="px-3 py-2 text-xs bg-red-600 hover:bg-red-700 text-white rounded-lg disabled:opacity-50"
                      >
                        Delete
                      </button>
                      <button onClick={() => setConfirmDelete(null)} className="text-xs text-gray-400 hover:text-gray-200">
                        Cancel
                      </button>
                    </span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </>
  );
}
