const TOKEN_KEY = 'batter_token';
const REFRESH_KEY = 'batter_refresh';
const USER_KEY = 'batter_user';

export interface User {
  id: string;
  username: string;
  display_name: string;
  role: 'admin' | 'operator' | 'viewer';
}

export function getToken(): string | null {
  if (typeof window === 'undefined') return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function getRefreshToken(): string | null {
  if (typeof window === 'undefined') return null;
  return localStorage.getItem(REFRESH_KEY);
}

export function getUser(): User | null {
  if (typeof window === 'undefined') return null;
  const raw = localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function setAuth(accessToken: string, refreshToken: string, user: User) {
  localStorage.setItem(TOKEN_KEY, accessToken);
  localStorage.setItem(REFRESH_KEY, refreshToken);
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
  localStorage.removeItem(USER_KEY);
}

export function isAdmin(): boolean {
  const user = getUser();
  return user?.role === 'admin';
}

// Operators and admins may register phones and move them to the network.
export function isOperator(): boolean {
  const role = getUser()?.role;
  return role === 'admin' || role === 'operator';
}
