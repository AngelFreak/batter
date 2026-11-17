'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { login, checkNeedsSetup, setup } from '@/lib/api';
import { setAuth, getToken } from '@/lib/auth';
import { errorMessage } from '@/lib/api-error';
import { Logomark } from '@/components/layout/sidebar';
import { Button, Field, FormError } from '@/components/ui';

export default function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [email, setEmail] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    if (getToken()) {
      router.replace('/dashboard');
      return;
    }
    checkNeedsSetup()
      .then(setNeedsSetup)
      .catch(() => {})
      .finally(() => setChecking(false));
  }, [router]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    const firstRun = needsSetup;
    try {
      if (needsSetup) {
        await setup(username, password, email || undefined);
        setNeedsSetup(false);
        // After setup, log in
      }

      const data = await login(username, password);
      setAuth(data.access_token, data.refresh_token, data.user);
      // First run: offer the phone network's port (optional) before the dashboard.
      router.push(firstRun ? '/admin/phone-network?setup=1' : '/dashboard');
    } catch (err) {
      setError(errorMessage(err, firstRun ? 'create the account' : 'sign in'));
    } finally {
      setLoading(false);
    }
  };

  if (checking) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-vx-canvas">
        <div role="status" className="flex items-center gap-3 text-vx-fg-muted">
          <Logomark />
          <span className="vx-label">Loading</span>
        </div>
      </div>
    );
  }

  return (
    <main className="flex min-h-dvh items-center justify-center bg-vx-canvas px-4 py-8">
      <div className="w-full max-w-[400px] border border-vx-hairline bg-vx-surface rounded-md">
        <div className="border-b border-vx-hairline px-6 pb-5 pt-6 sm:px-8">
          <div className="flex items-center gap-3">
            <Logomark className="h-8 w-8" />
            <span className="text-xl font-extrabold tracking-wide text-vx-fg">Batter</span>
          </div>
          <h1 className="mt-5 text-[26px] leading-tight">
            {needsSetup ? 'Create the admin account' : 'Sign in'}
          </h1>
          <p className="mt-1.5 text-sm text-vx-fg-muted">
            {needsSetup
              ? 'First run. This account manages users, phones and the phone network.'
              : 'Remote phone management.'}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-5 px-6 py-6 sm:px-8">
          <Field label="Username">
            <input
              type="text"
              name="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="vx-input"
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              required
              autoFocus
            />
          </Field>

          {needsSetup && (
            <Field label="Email (optional)" hint="Used for account notices only.">
              <input
                type="email"
                name="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="vx-input"
                autoComplete="email"
              />
            </Field>
          )}

          <Field label="Password" hint={needsSetup ? 'At least 8 characters.' : undefined}>
            <input
              type="password"
              name="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="vx-input"
              autoComplete={needsSetup ? 'new-password' : 'current-password'}
              required
              minLength={8}
            />
          </Field>

          <FormError>{error}</FormError>

          <Button type="submit" variant="primary" size="lg" className="w-full" disabled={loading}>
            {loading ? 'Working…' : needsSetup ? 'Create admin and sign in' : 'Sign in'}
          </Button>
        </form>
      </div>
    </main>
  );
}
