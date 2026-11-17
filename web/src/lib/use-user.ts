'use client';

import { useEffect, useState } from 'react';
import { getUser, type User } from './auth';

export interface CurrentUser {
  user: User | null;
  /** False until read from storage on the client; render nothing role-gated before. */
  ready: boolean;
  isAdmin: boolean;
  /** Admins and operators: may register phones, run sessions, change the network. */
  isOperator: boolean;
  /** Viewers watch only. */
  isViewer: boolean;
}

/** The signed-in user and what their role allows, for hiding controls they can't use. */
export function useUser(): CurrentUser {
  const [user, setUser] = useState<User | null>(null);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    setUser(getUser());
    setReady(true);
  }, []);
  const role = user?.role;
  return {
    user,
    ready,
    isAdmin: role === 'admin',
    isOperator: role === 'admin' || role === 'operator',
    isViewer: role === 'viewer',
  };
}
