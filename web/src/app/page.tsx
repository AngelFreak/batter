'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { getToken } from '@/lib/auth';

export default function Home() {
  const router = useRouter();

  useEffect(() => {
    if (getToken()) {
      router.replace('/dashboard');
    } else {
      router.replace('/login');
    }
  }, [router]);

  return (
    <div className="flex min-h-dvh items-center justify-center bg-vx-canvas">
      <div role="status" className="vx-label">
        Loading
      </div>
    </div>
  );
}
