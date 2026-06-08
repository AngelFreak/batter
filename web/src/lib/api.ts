import { getToken, getRefreshToken, setAuth, clearAuth } from './auth';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || '';

async function fetchWithAuth(path: string, options: RequestInit = {}): Promise<Response> {
  const token = getToken();
  const headers = new Headers(options.headers);

  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  // Only default to JSON for non-FormData bodies — the browser must set the
  // multipart Content-Type (with boundary) itself for FormData uploads.
  if (!headers.has('Content-Type') && options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json');
  }

  let response = await fetch(`${API_BASE}${path}`, { ...options, headers });

  // Try refresh on 401
  if (response.status === 401 && getRefreshToken()) {
    const refreshed = await refreshAccessToken();
    if (refreshed) {
      headers.set('Authorization', `Bearer ${getToken()}`);
      response = await fetch(`${API_BASE}${path}`, { ...options, headers });
    } else {
      clearAuth();
      window.location.href = '/login';
    }
  }

  return response;
}

async function refreshAccessToken(): Promise<boolean> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) return false;

  try {
    const res = await fetch(`${API_BASE}/api/v1/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });

    if (!res.ok) return false;

    const data = await res.json();
    const user = JSON.parse(localStorage.getItem('batter_user') || '{}');
    setAuth(data.access_token, refreshToken, user);
    return true;
  } catch {
    return false;
  }
}

// Auth API
export async function login(username: string, password: string) {
  const res = await fetch(`${API_BASE}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Login failed');
  }
  return res.json();
}

export async function checkNeedsSetup(): Promise<boolean> {
  const res = await fetch(`${API_BASE}/api/v1/auth/needs-setup`);
  const data = await res.json();
  return data.needs_setup;
}

export async function setup(username: string, password: string, email?: string) {
  const res = await fetch(`${API_BASE}/api/v1/admin/setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password, email }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Setup failed');
  }
  return res.json();
}

export async function logout() {
  const refreshToken = getRefreshToken();
  if (refreshToken) {
    await fetch(`${API_BASE}/api/v1/auth/logout`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    }).catch(() => {});
  }
  clearAuth();
}

// Device API
export interface DeviceInfo {
  serial: string;
  state: string;
  status: 'connected' | 'disconnected' | 'offline' | 'unauthorized';
  model: string;
  product: string;
  nickname?: string;
  android_version?: string;
  has_session: boolean;
  width?: number;
  height?: number;
  session_tier?: 'thumbnail' | 'full';
  last_seen_at?: string;
}

export async function pushFile(serial: string, file: File) {
  const formData = new FormData();
  formData.append('file', file);
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/push`, {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to push file');
  }
  return res.json();
}

export async function installAPK(serial: string, file: File) {
  const formData = new FormData();
  formData.append('file', file);
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/install`, {
    method: 'POST',
    body: formData,
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to install APK');
  }
  return res.json();
}

export async function fetchScreenshot(serial: string): Promise<Blob | null> {
  try {
    const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/screenshot`);
    if (!res.ok) return null;
    return await res.blob();
  } catch {
    return null;
  }
}

export async function listDevices(): Promise<DeviceInfo[]> {
  const res = await fetchWithAuth('/api/v1/devices');
  if (!res.ok) throw new Error('Failed to list devices');
  const data = await res.json();
  return data.devices;
}

export async function getDevice(serial: string): Promise<DeviceInfo> {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}`);
  if (!res.ok) throw new Error('Failed to get device');
  return res.json();
}

export async function startSession(serial: string, maxSize?: number, maxFps?: number) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/session/start`, {
    method: 'POST',
    body: JSON.stringify({ max_size: maxSize, max_fps: maxFps }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to start session');
  }
  return res.json();
}

export async function stopSession(serial: string) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/session/stop`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to stop session');
  return res.json();
}

export async function upgradeSession(serial: string) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/session/upgrade`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to upgrade session');
  return res.json();
}

export async function downgradeSession(serial: string) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/session/downgrade`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to downgrade session');
  return res.json();
}

export async function wakeDevice(serial: string) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}/wake`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to wake device');
  return res.json();
}

export async function getDeviceHealth() {
  const res = await fetchWithAuth('/api/v1/devices/health');
  if (!res.ok) throw new Error('Failed to get health');
  return res.json();
}

export async function discoverDevices(): Promise<DeviceInfo[]> {
  const res = await fetchWithAuth('/api/v1/devices/discover');
  if (!res.ok) throw new Error('Failed to discover devices');
  const data = await res.json();
  return data.devices;
}

export async function registerDevice(serial: string, nickname?: string) {
  const res = await fetchWithAuth('/api/v1/devices', {
    method: 'POST',
    body: JSON.stringify({ serial, nickname }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to register device');
  }
  return res.json();
}

export async function validateDevice(serial: string): Promise<{ serial: string; reachable: boolean; state: string; error?: string }> {
  const res = await fetchWithAuth(`/api/v1/devices/validate/${encodeURIComponent(serial)}`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to validate device');
  return res.json();
}

export async function probeDevice(serial: string): Promise<{ serial: string; model: string; product: string; android_version: string }> {
  const res = await fetchWithAuth(`/api/v1/devices/probe/${encodeURIComponent(serial)}`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to probe device');
  return res.json();
}

export async function updateDevice(serial: string, data: { nickname?: string; model?: string; product?: string }) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
  if (!res.ok) throw new Error('Failed to update device');
  return res.json();
}

export async function deleteDevice(serial: string) {
  const res = await fetchWithAuth(`/api/v1/devices/${encodeURIComponent(serial)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error('Failed to delete device');
  return res.json();
}

// User API
export interface UserInfo {
  id: string;
  username: string;
  email: string;
  display_name: string;
  role: string;
  is_active: boolean;
  last_login_at: string | null;
  created_at: string;
}

export interface UserAccessGrant {
  id: string;
  device_serial?: string;
  group_id?: string;
  group_name?: string;
  permission: string;
  created_at: string;
}

export async function listUsers(): Promise<UserInfo[]> {
  const res = await fetchWithAuth('/api/v1/users');
  if (!res.ok) throw new Error('Failed to list users');
  const data = await res.json();
  return data.users;
}

export async function createUser(username: string, password: string, role: string): Promise<{ id: string }> {
  const res = await fetchWithAuth('/api/v1/users', {
    method: 'POST',
    body: JSON.stringify({ username, password, role }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to create user');
  }
  return res.json();
}

export async function updateUser(userId: string, data: { role?: string; display_name?: string; is_active?: boolean }) {
  const res = await fetchWithAuth(`/api/v1/users/${userId}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to update user');
  }
  return res.json();
}

export async function deleteUser(userId: string) {
  const res = await fetchWithAuth(`/api/v1/users/${userId}`, { method: 'DELETE' });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to delete user');
  }
  return res.json();
}

export async function listUserAccess(userId: string): Promise<UserAccessGrant[]> {
  const res = await fetchWithAuth(`/api/v1/users/${userId}/devices`);
  if (!res.ok) throw new Error('Failed to list user access');
  const data = await res.json();
  return data.grants;
}

export async function revokeUserAccess(userId: string, accessId: string) {
  const res = await fetchWithAuth(`/api/v1/users/${userId}/devices/${accessId}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to revoke access');
  return res.json();
}

export async function resetPassword(userId: string, newPassword: string) {
  const res = await fetchWithAuth(`/api/v1/users/${userId}/password`, {
    method: 'PUT',
    body: JSON.stringify({ new_password: newPassword }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to reset password');
  }
  return res.json();
}

export async function grantAccess(userId: string, data: { device_serial?: string; group_id?: string; permission: string }) {
  const res = await fetchWithAuth(`/api/v1/users/${userId}/devices`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
  if (!res.ok) throw new Error('Failed to grant access');
  return res.json();
}

// Group API
export interface DeviceGroup {
  id: string;
  name: string;
  description: string;
  color: string;
  member_count: number;
  created_at: string;
}

export async function listGroups(): Promise<DeviceGroup[]> {
  const res = await fetchWithAuth('/api/v1/groups');
  if (!res.ok) throw new Error('Failed to list groups');
  const data = await res.json();
  return data.groups;
}

export async function createGroup(name: string, description?: string, color?: string) {
  const res = await fetchWithAuth('/api/v1/groups', {
    method: 'POST',
    body: JSON.stringify({ name, description, color }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to create group');
  }
  return res.json();
}

export async function updateGroup(id: string, data: { name?: string; description?: string; color?: string }) {
  const res = await fetchWithAuth(`/api/v1/groups/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to update group');
  }
  return res.json();
}

export async function deleteGroup(id: string) {
  const res = await fetchWithAuth(`/api/v1/groups/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete group');
  return res.json();
}

export async function getGroupDevices(id: string): Promise<string[]> {
  const res = await fetchWithAuth(`/api/v1/groups/${id}/devices`);
  if (!res.ok) throw new Error('Failed to get group devices');
  const data = await res.json();
  return data.serials;
}

export async function addDevicesToGroup(id: string, serials: string[]) {
  const res = await fetchWithAuth(`/api/v1/groups/${id}/devices`, {
    method: 'POST',
    body: JSON.stringify({ serials }),
  });
  if (!res.ok) throw new Error('Failed to add devices');
  return res.json();
}

export async function batchStartGroup(id: string, maxSize?: number, maxFps?: number) {
  const res = await fetchWithAuth(`/api/v1/groups/${id}/batch/start`, {
    method: 'POST',
    body: JSON.stringify({ max_size: maxSize, max_fps: maxFps }),
  });
  if (!res.ok) throw new Error('Failed to batch start');
  return res.json();
}

export async function batchStopGroup(id: string) {
  const res = await fetchWithAuth(`/api/v1/groups/${id}/batch/stop`, { method: 'POST' });
  if (!res.ok) throw new Error('Failed to batch stop');
  return res.json();
}

export async function removeDeviceFromGroup(id: string, serial: string) {
  const res = await fetchWithAuth(`/api/v1/groups/${id}/devices/${encodeURIComponent(serial)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error('Failed to remove device from group');
  return res.json();
}

export interface GroupAccessGrant {
  id: string;
  user_id: string;
  username: string;
  permission: string;
  created_at: string;
}

export async function listGroupAccess(groupId: string): Promise<GroupAccessGrant[]> {
  const res = await fetchWithAuth(`/api/v1/groups/${groupId}/access`);
  if (!res.ok) throw new Error('Failed to list group access');
  const data = await res.json();
  return data.grants;
}

export async function revokeAccess(groupId: string, accessId: string) {
  const res = await fetchWithAuth(`/api/v1/groups/${groupId}/access/${accessId}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error('Failed to revoke access');
  return res.json();
}

export interface GroupTeamAccessGrant {
  id: string;
  user_group_id: string;
  team_name: string;
  permission: string;
  created_at: string;
}

export async function listGroupTeamAccess(groupId: string): Promise<GroupTeamAccessGrant[]> {
  const res = await fetchWithAuth(`/api/v1/groups/${groupId}/team-access`);
  if (!res.ok) throw new Error('Failed to list team access');
  const data = await res.json();
  return data.grants;
}

export async function grantGroupTeamAccess(groupId: string, userGroupId: string, permission: string) {
  const res = await fetchWithAuth(`/api/v1/groups/${groupId}/team-access`, {
    method: 'POST',
    body: JSON.stringify({ user_group_id: userGroupId, permission }),
  });
  if (!res.ok) throw new Error('Failed to grant team access');
  return res.json();
}

export async function revokeGroupTeamAccess(groupId: string, accessId: string) {
  const res = await fetchWithAuth(`/api/v1/groups/${groupId}/team-access/${accessId}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error('Failed to revoke team access');
  return res.json();
}

// User Group (Team) API
export interface UserGroup {
  id: string;
  name: string;
  description: string;
  member_count: number;
  created_at: string;
}

export interface UserGroupMember {
  user_id: string;
  username: string;
  display_name: string;
  role: string;
}

export interface UserGroupAccessGrant {
  id: string;
  device_serial?: string;
  group_id?: string;
  group_name?: string;
  permission: string;
  created_at: string;
}

export async function listUserGroups(): Promise<UserGroup[]> {
  const res = await fetchWithAuth('/api/v1/user-groups');
  if (!res.ok) throw new Error('Failed to list user groups');
  const data = await res.json();
  return data.groups;
}

export async function createUserGroup(name: string, description?: string): Promise<UserGroup> {
  const res = await fetchWithAuth('/api/v1/user-groups', {
    method: 'POST',
    body: JSON.stringify({ name, description }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to create group');
  }
  return res.json();
}

export async function updateUserGroup(id: string, data: { name?: string; description?: string }) {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to update group');
  }
  return res.json();
}

export async function deleteUserGroup(id: string) {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete group');
  return res.json();
}

export async function listUserGroupMembers(id: string): Promise<UserGroupMember[]> {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}/members`);
  if (!res.ok) throw new Error('Failed to list members');
  const data = await res.json();
  return data.members;
}

export async function addUserGroupMember(id: string, userId: string) {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}/members`, {
    method: 'POST',
    body: JSON.stringify({ user_id: userId }),
  });
  if (!res.ok) throw new Error('Failed to add member');
  return res.json();
}

export async function removeUserGroupMember(id: string, userId: string) {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}/members/${userId}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error('Failed to remove member');
  return res.json();
}

export async function listUserGroupAccess(id: string): Promise<UserGroupAccessGrant[]> {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}/access`);
  if (!res.ok) throw new Error('Failed to list access');
  const data = await res.json();
  return data.grants;
}

export async function grantUserGroupAccess(id: string, body: { device_serial?: string; group_id?: string; permission: string }) {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}/access`, {
    method: 'POST',
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error('Failed to grant access');
  return res.json();
}

export async function revokeUserGroupAccess(id: string, accessId: string) {
  const res = await fetchWithAuth(`/api/v1/user-groups/${id}/access/${accessId}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error('Failed to revoke access');
  return res.json();
}
