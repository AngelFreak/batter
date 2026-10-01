'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { getToken, isAdmin } from '@/lib/auth';
import {
  getPhoneNetwork, listBoxNICs, setPhoneNetworkPort,
  type BoxNIC, type PhoneNetworkStatus,
} from '@/lib/api';
import { describeNIC, pickable, stateSummary } from '@/lib/phone-network';

const toneClass = {
  ok: 'bg-green-900/30 text-green-300 border-green-700',
  off: 'bg-gray-800 text-gray-300 border-gray-700',
  bad: 'bg-yellow-900/30 text-yellow-300 border-yellow-700',
};

export default function PhoneNetworkPage() {
  const router = useRouter();
  const [status, setStatus] = useState<PhoneNetworkStatus | null>(null);
  const [nics, setNics] = useState<BoxNIC[] | null>(null);
  const [nicError, setNicError] = useState('');
  const [choice, setChoice] = useState<string>('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // First-run: shown right after the admin account is created.
  const [setup, setSetup] = useState(false);

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
    setSetup(new URLSearchParams(window.location.search).has('setup'));
  }, [router]);

  const load = useCallback(async () => {
    try {
      const st = await getPhoneNetwork();
      setStatus(st);
      setChoice((c) => c || st.port?.mac || '');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    }
    try {
      setNics(await listBoxNICs());
      setNicError('');
    } catch (e) {
      setNics([]);
      setNicError(e instanceof Error ? e.message : 'Failed to list network ports');
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [load]);

  const apply = async (mac: string | null) => {
    setBusy(true);
    setError('');
    try {
      setStatus(await setPhoneNetworkPort(mac));
      setChoice(mac ?? '');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to change the phone network');
    } finally {
      setBusy(false);
    }
  };

  const summary = status ? stateSummary(status) : null;
  const usable = pickable(nics ?? []);
  const refused = (nics ?? []).filter((n) => !n.usable);

  return (
    <>
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
        <div>
          <h2 className="text-lg font-semibold text-white">Phone network</h2>
          <p className="text-xs text-gray-500">The box&apos;s network port for the phones&apos; switch</p>
        </div>
        {setup && (
          <button
            onClick={() => router.push('/dashboard')}
            className="px-2.5 py-1.5 rounded border border-gray-700 text-gray-300 text-xs hover:bg-gray-800"
          >
            {status?.state === 'active' ? 'Continue' : 'Skip for now'}
          </button>
        )}
      </div>

      <div className="flex-1 overflow-auto">
        <div className="max-w-3xl mx-auto p-6 space-y-4">
          {setup && (
            <p className="text-xs text-gray-300">
              Optional: if the phones will use ethernet adapters, choose the network port their switch is plugged
              into. You can do this later under Admin → Phone network.
            </p>
          )}
          <p className="text-xs text-gray-400">
            Phones with an ethernet adapter, on a switch plugged into this port, get their network from Batter and
            reach the internet only through their VPN profile. While Batter uses the port, the box itself has no
            connection on it. Phones on USB are controlled only and get no internet.
          </p>

          {status && summary && (
            <div className="rounded-lg bg-gray-900 border border-gray-800 p-3 space-y-2">
              <div className="flex items-center gap-2">
                <span className={`inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] border ${toneClass[summary.tone]}`}>
                  {summary.label}
                </span>
                {status.state === 'active' && (
                  <span className="text-[10px] text-gray-500">
                    Batter {status.address} · phones {status.pool}
                  </span>
                )}
              </div>
              {status.error && <p className="text-xs text-red-400">{status.error}</p>}
              {status.unavailable && <p className="text-xs text-red-400">{status.unavailable}</p>}
              {status.warning && <p className="text-xs text-yellow-400">{status.warning}</p>}
            </div>
          )}

          <div className="rounded-lg bg-gray-900 border border-gray-800 p-3 space-y-2">
            <div className="text-sm text-white">Port</div>
            {nicError && <p className="text-xs text-red-400">{nicError}</p>}
            <label className="flex items-center gap-2 text-xs text-gray-300">
              <input type="radio" name="port" checked={choice === ''} onChange={() => setChoice('')} disabled={busy} />
              Off (no phone network)
            </label>
            {usable.map((n) => (
              <label key={n.mac} className="flex items-center gap-2 text-xs text-gray-300">
                <input type="radio" name="port" checked={choice === n.mac} onChange={() => setChoice(n.mac)} disabled={busy} />
                <span className="font-mono">{n.name}</span>
                <span className="text-[10px] text-gray-500">{n.mac} · {describeNIC(n)}</span>
              </label>
            ))}
            {refused.length > 0 && (
              <div className="pt-2 space-y-1">
                <div className="text-[10px] text-gray-500">Can&apos;t be used:</div>
                {refused.map((n) => (
                  <div key={n.mac} className="text-[10px] text-gray-500">
                    <span className="font-mono text-gray-400">{n.name}</span> ({n.mac}): {n.reason}
                  </div>
                ))}
              </div>
            )}
            <div className="flex items-center gap-2 pt-1">
              <button
                onClick={() => apply(choice || null)}
                disabled={busy || choice === (status?.port?.mac ?? '')}
                className="px-2.5 py-1.5 rounded bg-brand-600 text-white text-xs hover:bg-brand-700 disabled:opacity-50"
              >
                {busy ? 'Applying...' : 'Apply'}
              </button>
              {error && <span className="text-xs text-red-400">{error}</span>}
            </div>
            <p className="text-[10px] text-gray-500">
              The box must not manage this port itself (no NetworkManager, netplan or ifupdown configuration): see
              docs/DEPLOY.md. Applying takes effect at once.
            </p>
          </div>

          {status && status.state === 'active' && (
            <div className="rounded-lg bg-gray-900 border border-gray-800 p-3 space-y-2">
              <div className="text-sm text-white">Devices on the network</div>
              {status.clients.length === 0 ? (
                <p className="text-xs text-gray-500">None yet.</p>
              ) : (
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-[10px] text-gray-500 text-left">
                      <th className="font-normal pb-1">Address</th>
                      <th className="font-normal pb-1">MAC</th>
                      <th className="font-normal pb-1">Name</th>
                      <th className="font-normal pb-1">Phone</th>
                    </tr>
                  </thead>
                  <tbody>
                    {status.clients.map((c) => (
                      <tr key={c.mac} className="text-gray-300">
                        <td className="font-mono py-0.5">{c.ip}</td>
                        <td className="font-mono py-0.5">{c.mac}</td>
                        <td className="py-0.5">{c.hostname || '—'}</td>
                        <td className="py-0.5">
                          {c.serial ? (
                            <span className="font-mono">{c.serial}</span>
                          ) : (
                            <span className="text-gray-500">not a phone (no internet)</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
}
