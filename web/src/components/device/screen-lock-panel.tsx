'use client';

import { useEffect, useState } from 'react';
import { getScreenLock, removeScreenLock, ScreenLockState } from '@/lib/api';

const CREDENTIAL_LABELS: Record<string, string> = {
  PIN: 'PIN',
  PASSWORD: 'password',
  PATTERN: 'pattern',
};

/**
 * Shows a phone's screen lock and offers to remove it. A dedicated Batter
 * phone shouldn't make remote users unlock it first.
 */
export function ScreenLockPanel({ serial }: { serial: string }) {
  const [state, setState] = useState<ScreenLockState | null>(null);
  const [credential, setCredential] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getScreenLock(serial)
      .then((s) => !cancelled && setState(s))
      .catch((e) => !cancelled && setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [serial]);

  const remove = async () => {
    setBusy(true);
    setError(null);
    try {
      setState(await removeScreenLock(serial, credential));
      setCredential('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to remove screen lock');
    } finally {
      setBusy(false);
    }
  };

  if (!state) {
    return error ? (
      <p className="text-xs text-red-400">{error}</p>
    ) : (
      <div className="flex items-center gap-2">
        <div className="w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
        <span className="text-xs text-gray-400">Checking the phone&apos;s screen lock...</span>
      </div>
    );
  }

  if (state.credential === 'NONE' && state.disabled) {
    return <p className="text-xs text-green-400">No screen lock — remote users can use the phone straight away.</p>;
  }

  const locked = state.credential !== 'NONE';
  const kind = CREDENTIAL_LABELS[state.credential] ?? 'screen lock';

  return (
    <div className="space-y-3">
      <p className="text-xs text-gray-300">
        {locked ? (
          <>This phone is locked with a <strong>{kind}</strong>.</>
        ) : (
          <>This phone has no PIN, but still shows a swipe-to-unlock screen.</>
        )}{' '}
        For a phone dedicated to Batter, remove it so remote users aren&apos;t stopped by the lock screen.
      </p>
      <p className="text-[10px] text-gray-500">
        Only do this for phones without personal data or apps that need a screen lock (banking, wallet, work
        profile): anyone with physical access can then use the phone.
      </p>
      {locked && (
        <div>
          <label className="block text-xs text-gray-400 mb-1.5">Current {kind}</label>
          <input
            type="password"
            autoComplete="off"
            value={credential}
            onChange={(e) => setCredential(e.target.value)}
            placeholder={state.credential === 'PATTERN' ? 'Dots as digits 1–9, row by row' : ''}
            className="w-full px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-lg text-gray-200 placeholder-gray-600 focus:outline-none focus:border-brand-500"
          />
          <p className="text-[10px] text-yellow-500/80 mt-1">
            A wrong entry counts as a failed unlock attempt on the phone.
          </p>
        </div>
      )}
      {error && <p className="text-xs text-red-400">{error}</p>}
      <button
        onClick={remove}
        disabled={busy || (locked && !credential)}
        className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-200 border border-gray-700 rounded-lg disabled:opacity-50"
      >
        {busy ? 'Removing...' : locked ? 'Remove screen lock' : 'Remove swipe screen'}
      </button>
    </div>
  );
}
