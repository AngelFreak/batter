'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { login, checkNeedsSetup, setup } from '@/lib/api';
import { setAuth, getToken } from '@/lib/auth';

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

    try {
      const firstRun = needsSetup;
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
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  if (checking) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-gray-500">Loading...</div>
      </div>
    );
  }

  return (
    <div className="flex items-center justify-center min-h-screen bg-gray-950">
      <div className="w-full max-w-sm p-8">
        <div className="text-center mb-8">
          <h1 className="text-2xl font-bold text-white">Batter</h1>
          <p className="text-sm text-gray-400 mt-1">
            {needsSetup ? 'Create admin account' : 'Remote Phone Management'}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-gray-400 mb-1">Username</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="w-full px-3 py-2 bg-gray-900 border border-gray-800 rounded-lg text-white placeholder-gray-600 focus:outline-none focus:border-brand-500"
              placeholder="admin"
              required
              autoFocus
            />
          </div>

          {needsSetup && (
            <div>
              <label className="block text-sm text-gray-400 mb-1">Email (optional)</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full px-3 py-2 bg-gray-900 border border-gray-800 rounded-lg text-white placeholder-gray-600 focus:outline-none focus:border-brand-500"
                placeholder="admin@example.com"
              />
            </div>
          )}

          <div>
            <label className="block text-sm text-gray-400 mb-1">Password</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full px-3 py-2 bg-gray-900 border border-gray-800 rounded-lg text-white placeholder-gray-600 focus:outline-none focus:border-brand-500"
              placeholder="Min 8 characters"
              required
              minLength={8}
            />
          </div>

          {error && (
            <div className="text-red-400 text-sm bg-red-900/20 border border-red-800/50 rounded-lg px-3 py-2">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full py-2 bg-brand-600 hover:bg-brand-700 text-white rounded-lg font-medium transition-colors disabled:opacity-50"
          >
            {loading ? 'Please wait...' : needsSetup ? 'Create Admin & Login' : 'Sign In'}
          </button>
        </form>
      </div>
    </div>
  );
}
