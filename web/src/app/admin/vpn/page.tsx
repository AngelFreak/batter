'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { getToken, isAdmin } from '@/lib/auth';
import { getVPN, setVPN, deleteVPN, checkVPNExitIP, type VPNInfo } from '@/lib/api';

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

export default function AdminVPNPage() {
  const router = useRouter();
  const [info, setInfo] = useState<VPNInfo | null>(null);
  const [configText, setConfigText] = useState('');
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [exit, setExit] = useState<{ exit_ip?: string; error?: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
  }, [router]);

  const refresh = useCallback(async () => {
    try {
      setInfo(await getVPN());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load VPN');
    }
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 5000);
    return () => clearInterval(t);
  }, [refresh]);

  const run = async (fn: () => Promise<VPNInfo>) => {
    setBusy(true);
    setError('');
    setExit(null);
    try {
      setInfo(await fn());
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Request failed');
      return false;
    } finally {
      setBusy(false);
    }
  };

  const handleSave = async () => {
    if (await run(() => setVPN(true, configText))) {
      setConfigText('');
      setEditing(false);
    }
  };

  const handleCheck = async () => {
    setBusy(true);
    setExit(null);
    try {
      setExit(await checkVPNExitIP());
    } catch (e) {
      setExit({ error: e instanceof Error ? e.message : 'Check failed' });
    } finally {
      setBusy(false);
    }
  };

  const showEditor = editing || (info !== null && !info.configured);
  const peerStatus = info?.status?.peers ?? [];

  return (
    <>
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
        <div>
          <h2 className="text-lg font-semibold text-white">VPN</h2>
          <p className="text-xs text-gray-500">WireGuard for tethered phones only</p>
        </div>
        {info?.configured && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-400">{info.enabled ? 'Enabled' : 'Disabled'}</span>
            <button
              role="switch"
              aria-checked={info.enabled}
              aria-label="VPN enabled"
              onClick={() => run(() => setVPN(!info.enabled))}
              disabled={busy}
              className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-50 ${
                info.enabled ? 'bg-brand-600' : 'bg-gray-700'
              }`}
            >
              <span
                className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${
                  info.enabled ? 'translate-x-4' : 'translate-x-0.5'
                }`}
              />
            </button>
          </div>
        )}
      </div>

      <div className="flex-1 overflow-auto">
        <div className="max-w-4xl mx-auto p-6 space-y-4">
          <p className="text-xs text-gray-400">
            Phones with reverse tethering on reach the internet through this tunnel. Nothing else
            uses it: Batter, adb and the host keep their normal route. While the VPN is enabled and
            the tunnel is down, tethered phones have no internet at all rather than going out
            directly. With no VPN saved, they use this server&apos;s normal internet.
          </p>

          {error && <div className="text-xs text-red-400">{error}</div>}
          {info?.apply_error && (
            <div className="text-xs text-yellow-400 bg-yellow-900/20 border border-yellow-800/50 rounded-lg p-3">
              Saved, but not fully applied: {info.apply_error}
            </div>
          )}

          {info?.configured && info.config && (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-3">
              <div className="grid grid-cols-2 gap-3 text-xs">
                <div>
                  <div className="text-[10px] text-gray-500 mb-1">Tunnel</div>
                  <div className={info.status?.up ? 'text-green-400' : 'text-gray-400'}>
                    {!info.enabled ? 'Off' : info.status?.up ? 'Up' : 'Down'}
                  </div>
                </div>
                <div>
                  <div className="text-[10px] text-gray-500 mb-1">Address</div>
                  <div className="text-gray-300 font-mono">{info.config.addresses.join(', ')}</div>
                </div>
                <div>
                  <div className="text-[10px] text-gray-500 mb-1">DNS for phones</div>
                  <div className="text-gray-300 font-mono">{info.config.dns?.join(', ') || 'default (8.8.8.8)'}</div>
                </div>
                <div>
                  <div className="text-[10px] text-gray-500 mb-1">This server&apos;s public key</div>
                  <div className="text-gray-300 font-mono break-all">{info.config.public_key}</div>
                </div>
              </div>

              {info.config.peers.map((p) => {
                const live = peerStatus.find((s) => s.public_key === p.public_key);
                return (
                  <div key={p.public_key} className="border-t border-gray-800 pt-3 grid grid-cols-2 gap-3 text-xs">
                    <div>
                      <div className="text-[10px] text-gray-500 mb-1">Endpoint</div>
                      <div className="text-gray-300 font-mono">{live?.endpoint || p.endpoint}</div>
                    </div>
                    <div>
                      <div className="text-[10px] text-gray-500 mb-1">Latest handshake</div>
                      <div className="text-gray-300">{info.enabled ? formatHandshake(live?.latest_handshake) : '-'}</div>
                    </div>
                    <div>
                      <div className="text-[10px] text-gray-500 mb-1">Received / sent</div>
                      <div className="text-gray-300">
                        {live ? `${formatBytes(live.rx_bytes)} / ${formatBytes(live.tx_bytes)}` : '-'}
                      </div>
                    </div>
                    <div>
                      <div className="text-[10px] text-gray-500 mb-1">Allowed IPs</div>
                      <div className="text-gray-300 font-mono">{p.allowed_ips.join(', ')}</div>
                    </div>
                  </div>
                );
              })}

              <div className="border-t border-gray-800 pt-3 flex flex-wrap items-center gap-2">
                <button
                  onClick={handleCheck}
                  disabled={busy}
                  className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-200 rounded-lg disabled:opacity-50"
                >
                  Check exit IP
                </button>
                {exit?.exit_ip && (
                  <span className="text-xs text-gray-300">
                    Tethered phones exit from <span className="font-mono text-white">{exit.exit_ip}</span>
                  </span>
                )}
                {exit?.error && <span className="text-xs text-red-400">No internet for tethered phones: {exit.error}</span>}
              </div>
            </div>
          )}

          {showEditor ? (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-3">
              <label className="block text-xs text-gray-400">
                {info?.configured ? 'Replace config' : 'WireGuard config'}
              </label>
              <textarea
                value={configText}
                onChange={(e) => setConfigText(e.target.value)}
                rows={12}
                spellCheck={false}
                placeholder={'[Interface]\nPrivateKey = ...\nAddress = 10.64.0.2/32\nDNS = 10.64.0.1\n\n[Peer]\nPublicKey = ...\nEndpoint = vpn.example.net:51820\nAllowedIPs = 0.0.0.0/0'}
                className={`${inputClass} font-mono`}
              />
              <p className="text-[10px] text-gray-500">
                Paste a standard wg-quick .conf from your VPN provider. The private key is stored on
                this server only and is never shown again. Saving enables the VPN.
              </p>
              <div className="flex gap-2">
                <button
                  onClick={handleSave}
                  disabled={busy || !configText.trim()}
                  className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50"
                >
                  {busy ? 'Saving...' : 'Save and enable'}
                </button>
                {info?.configured && (
                  <button
                    onClick={() => { setEditing(false); setConfigText(''); }}
                    className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg"
                  >
                    Cancel
                  </button>
                )}
              </div>
            </div>
          ) : (
            info?.configured && (
              <div className="flex items-center gap-3">
                <button onClick={() => setEditing(true)} className="text-xs text-brand-400 hover:text-brand-300">
                  Replace config
                </button>
                {!confirmDelete ? (
                  <button onClick={() => setConfirmDelete(true)} className="text-xs text-red-400 hover:text-red-300">
                    Remove VPN
                  </button>
                ) : (
                  <span className="flex items-center gap-2">
                    <span className="text-xs text-red-400">Tethered phones will use this server&apos;s normal internet.</span>
                    <button
                      onClick={async () => { await run(deleteVPN); setConfirmDelete(false); }}
                      disabled={busy}
                      className="px-3 py-2 text-xs bg-red-600 hover:bg-red-700 text-white rounded-lg disabled:opacity-50"
                    >
                      Remove
                    </button>
                    <button onClick={() => setConfirmDelete(false)} className="text-xs text-gray-400 hover:text-gray-200">
                      Cancel
                    </button>
                  </span>
                )}
              </div>
            )
          )}
        </div>
      </div>
    </>
  );
}
