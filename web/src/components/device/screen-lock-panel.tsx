'use client';

import { useEffect, useState } from 'react';
import { LoaderCircle } from 'lucide-react';
import { getScreenLock, removeScreenLock, ScreenLockState } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { Banner, Button, Field, FormError, useToast } from '@/components/ui';

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
  const toast = useToast();
  const [state, setState] = useState<ScreenLockState | null>(null);
  const [credential, setCredential] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getScreenLock(serial)
      .then((s) => !cancelled && setState(s))
      .catch((e) => !cancelled && setError(errorMessage(e, "read the phone's screen lock")));
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
      toast.success('Screen lock removed');
    } catch (e) {
      setError(errorMessage(e, 'remove the screen lock'));
    } finally {
      setBusy(false);
    }
  };

  if (!state) {
    return error ? (
      <FormError>{error}</FormError>
    ) : (
      <p role="status" className="flex items-center gap-2 text-sm text-vx-fg-muted">
        <LoaderCircle size={16} className="animate-spin text-vx-accent" aria-hidden />
        Checking the phone&apos;s screen lock…
      </p>
    );
  }

  if (state.credential === 'NONE' && state.disabled) {
    return (
      <Banner tone="info">No screen lock. Remote users can use the phone straight away.</Banner>
    );
  }

  const locked = state.credential !== 'NONE';
  const kind = CREDENTIAL_LABELS[state.credential] ?? 'screen lock';

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!busy && (!locked || credential)) void remove();
      }}
    >
      <p className="text-sm text-vx-fg">
        {locked ? (
          <>
            This phone is locked with a <strong>{kind}</strong>.
          </>
        ) : (
          <>This phone has no PIN, but still shows a swipe-to-unlock screen.</>
        )}{' '}
        For a phone dedicated to Batter, remove it so remote users aren&apos;t stopped by the lock screen.
      </p>
      <Banner tone="warn">
        Only do this for phones without personal data or apps that need a screen lock (banking, wallet, work profile).
        Anyone with physical access can then use the phone.
      </Banner>
      {locked && (
        <Field
          label={`Current ${kind}`}
          hint={
            state.credential === 'PATTERN'
              ? 'Enter the dots as digits 1 to 9, row by row. A wrong entry counts as a failed unlock attempt on the phone.'
              : 'A wrong entry counts as a failed unlock attempt on the phone.'
          }
        >
          <input
            type="password"
            autoComplete="off"
            value={credential}
            onChange={(e) => setCredential(e.target.value)}
            className="vx-input"
          />
        </Field>
      )}
      <FormError>{error}</FormError>
      <Button type="submit" disabled={busy || (locked && !credential)}>
        {busy ? 'Removing…' : locked ? 'Remove screen lock' : 'Remove swipe screen'}
      </Button>
    </form>
  );
}
