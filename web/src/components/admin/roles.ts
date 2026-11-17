// What each role can do, in words an admin can read before changing one.
// Mirrors the route guards in internal/api/router.go.
export type Role = 'admin' | 'operator' | 'viewer';

export const ROLES: Role[] = ['viewer', 'operator', 'admin'];

export const ROLE_LABEL: Record<Role, string> = {
  admin: 'Admin',
  operator: 'Operator',
  viewer: 'Viewer',
};

/** Completes "A viewer can ...". */
export const ROLE_CAN: Record<Role, string> = {
  admin: 'do everything, including managing users and settings',
  operator: 'register phones, run sessions and change the phone network',
  viewer: "watch and use the phones they're granted",
};

export function roleArticle(role: Role): 'a' | 'an' {
  return role === 'viewer' ? 'a' : 'an';
}

export function isRole(v: string): v is Role {
  return v === 'admin' || v === 'operator' || v === 'viewer';
}
