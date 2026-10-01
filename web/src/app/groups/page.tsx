'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import { useRouter } from 'next/navigation';
import {
  listGroups, createGroup, updateGroup, deleteGroup, listDevices,
  addDevicesToGroup, removeDeviceFromGroup, batchStartGroup, batchStopGroup,
  getGroupDevices, listUsers, grantAccess, listGroupAccess, revokeAccess,
  listGroupTeamAccess, grantGroupTeamAccess, revokeGroupTeamAccess,
  listUserGroups,
  DeviceGroup, DeviceInfo, UserInfo, GroupAccessGrant, GroupTeamAccessGrant, UserGroup,
} from '@/lib/api';
import { getToken, getUser } from '@/lib/auth';

// Avatar color palette based on first letter
function getAvatarColor(name: string): { bg: string; text: string } {
  const letter = (name || '?')[0].toUpperCase();
  const code = letter.charCodeAt(0);
  if (code <= 68) return { bg: 'bg-blue-600/20', text: 'text-blue-400' };
  if (code <= 72) return { bg: 'bg-green-600/20', text: 'text-green-400' };
  if (code <= 76) return { bg: 'bg-purple-600/20', text: 'text-purple-400' };
  if (code <= 80) return { bg: 'bg-orange-600/20', text: 'text-orange-400' };
  if (code <= 84) return { bg: 'bg-cyan-600/20', text: 'text-cyan-400' };
  return { bg: 'bg-pink-600/20', text: 'text-pink-400' };
}

function permissionBadgeClass(perm: string): string {
  if (perm === 'manage') return 'bg-red-900/30 text-red-400';
  if (perm === 'control') return 'bg-blue-900/30 text-blue-400';
  return 'bg-gray-800 text-gray-400';
}

const ChevronIcon = ({ expanded }: { expanded: boolean }) => (
  <svg className={`w-4 h-4 text-gray-500 transition-transform ${expanded ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
  </svg>
);

const SearchIcon = () => (
  <svg className="absolute left-2.5 top-2.5 w-3 h-3 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
  </svg>
);

const XIcon = () => (
  <svg className="w-3.5 h-3.5 text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
  </svg>
);

interface BatchResult {
  groupId: string;
  message: string;
  type: 'success' | 'error';
}

type Tab = 'devices' | 'access' | 'settings';

const COLOR_OPTIONS = [
  '#6366f1', '#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4',
];

export default function GroupsPage() {
  const router = useRouter();
  const [groups, setGroups] = useState<DeviceGroup[]>([]);
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [expandedGroupId, setExpandedGroupId] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>('devices');
  const [groupMembers, setGroupMembers] = useState<Record<string, string[]>>({});
  const [batchLoading, setBatchLoading] = useState<Record<string, boolean>>({});
  const [batchResults, setBatchResults] = useState<BatchResult[]>([]);

  // Device search
  const [deviceSearch, setDeviceSearch] = useState('');
  const [showDeviceDropdown, setShowDeviceDropdown] = useState(false);
  const deviceSearchRef = useRef<HTMLDivElement>(null);

  // Access management state (admin only)
  const [accessGrants, setAccessGrants] = useState<Record<string, GroupAccessGrant[]>>({});
  const [teamAccessGrants, setTeamAccessGrants] = useState<Record<string, GroupTeamAccessGrant[]>>({});
  const [users, setUsers] = useState<UserInfo[]>([]);
  const [teams, setTeams] = useState<UserGroup[]>([]);
  const [grantTarget, setGrantTarget] = useState<'user' | 'team'>('user');
  const [grantUserId, setGrantUserId] = useState('');
  const [grantTeamId, setGrantTeamId] = useState('');
  const [grantPermission, setGrantPermission] = useState('view');

  // Settings
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editColor, setEditColor] = useState('');
  const [settingsSuccess, setSettingsSuccess] = useState('');
  const [settingsError, setSettingsError] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);

  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    setIsAdmin(getUser()?.role === 'admin');
  }, [router]);

  // Close device dropdown on outside click
  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (deviceSearchRef.current && !deviceSearchRef.current.contains(e.target as Node)) {
        setShowDeviceDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  const refresh = useCallback(async () => {
    const [g, d] = await Promise.all([listGroups(), listDevices()]);
    setGroups(g);
    setDevices(d);
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    await createGroup(newName.trim(), newDesc.trim() || undefined);
    setNewName('');
    setNewDesc('');
    setShowCreate(false);
    refresh();
  };

  const handleBatchStart = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setBatchLoading(prev => ({ ...prev, [id]: true }));
    try {
      const result = await batchStartGroup(id, 360, 5);
      const msg = result.failed > 0
        ? `Started ${result.started}, failed ${result.failed}`
        : `Started ${result.started} device${result.started !== 1 ? 's' : ''}`;
      setBatchResults(prev => [...prev, { groupId: id, message: msg, type: result.failed > 0 ? 'error' : 'success' }]);
      refresh();
    } catch (err) {
      setBatchResults(prev => [...prev, { groupId: id, message: err instanceof Error ? err.message : 'Batch start failed', type: 'error' }]);
    } finally {
      setBatchLoading(prev => ({ ...prev, [id]: false }));
      setTimeout(() => setBatchResults(prev => prev.filter(r => r.groupId !== id)), 5000);
    }
  };

  const handleBatchStop = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setBatchLoading(prev => ({ ...prev, [id]: true }));
    try {
      const result = await batchStopGroup(id);
      const msg = result.failed > 0
        ? `Stopped ${result.stopped}, failed ${result.failed}`
        : `Stopped ${result.stopped} device${result.stopped !== 1 ? 's' : ''}`;
      setBatchResults(prev => [...prev, { groupId: id, message: msg, type: result.failed > 0 ? 'error' : 'success' }]);
      refresh();
    } catch (err) {
      setBatchResults(prev => [...prev, { groupId: id, message: err instanceof Error ? err.message : 'Batch stop failed', type: 'error' }]);
    } finally {
      setBatchLoading(prev => ({ ...prev, [id]: false }));
      setTimeout(() => setBatchResults(prev => prev.filter(r => r.groupId !== id)), 5000);
    }
  };

  const fetchGroupMembers = async (groupId: string) => {
    const serials = await getGroupDevices(groupId);
    setGroupMembers(prev => ({ ...prev, [groupId]: serials }));
  };

  const fetchGroupAccess = async (groupId: string) => {
    try {
      const [grants, teamGrants, userList, teamList] = await Promise.all([
        listGroupAccess(groupId),
        listGroupTeamAccess(groupId),
        users.length > 0 ? Promise.resolve(users) : listUsers(),
        teams.length > 0 ? Promise.resolve(teams) : listUserGroups(),
      ]);
      setAccessGrants(prev => ({ ...prev, [groupId]: grants }));
      setTeamAccessGrants(prev => ({ ...prev, [groupId]: teamGrants }));
      if (users.length === 0) setUsers(userList as UserInfo[]);
      if (teams.length === 0) setTeams(teamList as UserGroup[]);
    } catch {
      setAccessGrants(prev => ({ ...prev, [groupId]: [] }));
      setTeamAccessGrants(prev => ({ ...prev, [groupId]: [] }));
    }
  };

  const toggleExpand = (groupId: string) => {
    if (expandedGroupId === groupId) {
      setExpandedGroupId(null);
      return;
    }
    setExpandedGroupId(groupId);
    setActiveTab('devices');
    setConfirmDelete(false);
    setDeviceSearch('');
    fetchGroupMembers(groupId);
    if (isAdmin) fetchGroupAccess(groupId);

    const group = groups.find(g => g.id === groupId);
    if (group) {
      setEditName(group.name);
      setEditDesc(group.description || '');
      setEditColor(group.color);
    }
  };

  const handleAddDevice = async (groupId: string, serial: string) => {
    await addDevicesToGroup(groupId, [serial]);
    setDeviceSearch('');
    setShowDeviceDropdown(false);
    refresh();
    fetchGroupMembers(groupId);
  };

  const handleAddAllDevices = async (groupId: string) => {
    const connected = devices.filter(d => d.state === 'device').map(d => d.serial);
    if (connected.length > 0) {
      await addDevicesToGroup(groupId, connected);
      refresh();
      fetchGroupMembers(groupId);
    }
  };

  const handleRemoveDevice = async (groupId: string, serial: string) => {
    await removeDeviceFromGroup(groupId, serial);
    refresh();
    fetchGroupMembers(groupId);
  };

  const handleGrantAccess = async (groupId: string) => {
    if (grantTarget === 'user') {
      if (!grantUserId) return;
      await grantAccess(grantUserId, { group_id: groupId, permission: grantPermission });
      setGrantUserId('');
    } else {
      if (!grantTeamId) return;
      await grantGroupTeamAccess(groupId, grantTeamId, grantPermission);
      setGrantTeamId('');
    }
    setGrantPermission('view');
    fetchGroupAccess(groupId);
  };

  const handleRevokeTeamAccess = async (groupId: string, accessId: string) => {
    await revokeGroupTeamAccess(groupId, accessId);
    fetchGroupAccess(groupId);
  };

  const handleRevokeAccess = async (groupId: string, accessId: string) => {
    await revokeAccess(groupId, accessId);
    fetchGroupAccess(groupId);
  };

  const handleUpdateSettings = async (groupId: string) => {
    setSettingsError('');
    setSettingsSuccess('');
    try {
      await updateGroup(groupId, { name: editName, description: editDesc, color: editColor });
      setSettingsSuccess('Saved');
      refresh();
      setTimeout(() => setSettingsSuccess(''), 2000);
    } catch (e: unknown) {
      setSettingsError(e instanceof Error ? e.message : 'Failed to update');
    }
  };

  const handleDelete = async (id: string) => {
    await deleteGroup(id);
    if (expandedGroupId === id) setExpandedGroupId(null);
    refresh();
  };

  const getDeviceInfo = (serial: string) => devices.find(d => d.serial === serial);

  const getAvailableDevices = (groupId: string) => {
    const members = groupMembers[groupId] || [];
    return devices.filter(d => !members.includes(d.serial));
  };

  return (
    <>
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
        <div>
          <h2 className="text-lg font-semibold text-white">Device Groups</h2>
          <p className="text-xs text-gray-500">{groups.length} group{groups.length !== 1 ? 's' : ''}</p>
        </div>
        {isAdmin && (
          <button
            onClick={() => setShowCreate(true)}
            className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg"
          >
            New Group
          </button>
        )}
      </div>

      <div className="flex-1 overflow-auto">
        <div className="max-w-4xl mx-auto p-6 space-y-4">
          {showCreate && (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-3">
              <input
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="Group name"
                className="w-full px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white placeholder-gray-600 focus:outline-none focus:border-brand-600"
                autoFocus
              />
              <input
                type="text"
                value={newDesc}
                onChange={(e) => setNewDesc(e.target.value)}
                placeholder="Description (optional)"
                className="w-full px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white placeholder-gray-600 focus:outline-none focus:border-brand-600"
              />
              <div className="flex gap-2">
                <button onClick={handleCreate} className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg">Create</button>
                <button onClick={() => setShowCreate(false)} className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg">Cancel</button>
              </div>
            </div>
          )}

          {groups.length === 0 && !showCreate && (
            <div className="text-center py-12 text-gray-500 text-sm">
              {isAdmin ? 'No groups yet. Create one to organize your devices.' : 'No groups yet.'}
            </div>
          )}

          {groups.map(group => {
            const isExpanded = expandedGroupId === group.id;
            const colors = getAvatarColor(group.name);
            const members = groupMembers[group.id] || [];
            const grants = accessGrants[group.id] || [];
            const tGrants = teamAccessGrants[group.id] || [];
            const loading = batchLoading[group.id];
            const result = batchResults.find(r => r.groupId === group.id);

            return (
              <div key={group.id} className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
                {/* Card header */}
                <div
                  className="flex items-center justify-between px-5 py-4 cursor-pointer hover:bg-gray-800/30"
                  onClick={() => toggleExpand(group.id)}
                >
                  <div className="flex items-center gap-4">
                    <div className="relative">
                      <div className={`h-9 w-9 rounded-lg ${colors.bg} flex items-center justify-center ${colors.text} font-bold`}>
                        {group.name[0]?.toUpperCase()}
                      </div>
                      <div className="absolute -top-0.5 -left-0.5 w-3 h-3 rounded-full border-2 border-gray-900" style={{ backgroundColor: group.color }} />
                    </div>
                    <div>
                      <div className="text-white font-medium">{group.name}</div>
                      {group.description && (
                        <div className="text-xs text-gray-500">{group.description}</div>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="px-1.5 py-0.5 rounded text-[10px] bg-gray-800 text-gray-400">
                      {group.member_count} device{group.member_count !== 1 ? 's' : ''}
                    </span>
                    <button
                      onClick={(e) => handleBatchStart(group.id, e)}
                      disabled={loading}
                      className="px-2 py-1 text-[10px] bg-green-600/20 hover:bg-green-600/40 text-green-400 rounded disabled:opacity-50"
                    >
                      {loading ? '...' : 'Start All'}
                    </button>
                    <button
                      onClick={(e) => handleBatchStop(group.id, e)}
                      disabled={loading}
                      className="px-2 py-1 text-[10px] bg-yellow-600/20 hover:bg-yellow-600/40 text-yellow-400 rounded disabled:opacity-50"
                    >
                      {loading ? '...' : 'Stop All'}
                    </button>
                    <ChevronIcon expanded={isExpanded} />
                  </div>
                </div>

                {/* Batch result feedback */}
                {result && (
                  <div className={`mx-5 mb-2 text-xs px-2 py-1 rounded ${result.type === 'success' ? 'bg-green-600/20 text-green-400' : 'bg-red-600/20 text-red-400'}`}>
                    {result.message}
                  </div>
                )}

                {/* Expanded content */}
                {isExpanded && (
                  <>
                    {/* Tabs */}
                    <div className="flex border-t border-b border-gray-800 bg-gray-950/50">
                      <button
                        onClick={() => setActiveTab('devices')}
                        className={`px-5 py-2.5 text-xs font-medium border-b-2 ${
                          activeTab === 'devices' ? 'text-brand-400 border-brand-500' : 'text-gray-500 hover:text-gray-300 border-transparent'
                        }`}
                      >
                        Devices
                      </button>
                      {isAdmin && (
                        <button
                          onClick={() => setActiveTab('access')}
                          className={`px-5 py-2.5 text-xs font-medium border-b-2 ${
                            activeTab === 'access' ? 'text-brand-400 border-brand-500' : 'text-gray-500 hover:text-gray-300 border-transparent'
                          }`}
                        >
                          Access
                        </button>
                      )}
                      {isAdmin && (
                        <button
                          onClick={() => { setActiveTab('settings'); setConfirmDelete(false); }}
                          className={`px-5 py-2.5 text-xs font-medium border-b-2 ${
                            activeTab === 'settings' ? 'text-brand-400 border-brand-500' : 'text-gray-500 hover:text-gray-300 border-transparent'
                          }`}
                        >
                          Settings
                        </button>
                      )}
                    </div>

                    {/* Tab content */}
                    <div className="p-5">
                      {activeTab === 'devices' && (
                        <>
                          {/* Search to add device (admin-only, like the API) */}
                          {isAdmin && <div ref={deviceSearchRef} className="relative mb-4">
                            <div className="flex items-center gap-2">
                              <div className="relative flex-1">
                                <input
                                  type="text"
                                  value={deviceSearch}
                                  onChange={(e) => { setDeviceSearch(e.target.value); setShowDeviceDropdown(true); }}
                                  onFocus={() => setShowDeviceDropdown(true)}
                                  placeholder="Search devices to add..."
                                  className="w-full pl-8 pr-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white placeholder-gray-600 focus:outline-none focus:border-brand-600"
                                />
                                <SearchIcon />
                              </div>
                              <button
                                onClick={() => handleAddAllDevices(group.id)}
                                className="px-2.5 py-2 text-[10px] bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg whitespace-nowrap"
                              >
                                Add All Connected
                              </button>
                            </div>
                            {showDeviceDropdown && deviceSearch && (() => {
                              const available = getAvailableDevices(group.id).filter(d => {
                                const q = deviceSearch.toLowerCase();
                                return d.serial.toLowerCase().includes(q) ||
                                  (d.nickname || '').toLowerCase().includes(q) ||
                                  (d.model || '').toLowerCase().includes(q);
                              });
                              if (available.length === 0) return null;
                              return (
                                <div className="absolute z-10 w-full mt-1 bg-gray-950 border border-gray-800 rounded-lg shadow-lg max-h-48 overflow-auto">
                                  {available.slice(0, 8).map(d => (
                                    <button
                                      key={d.serial}
                                      onClick={() => handleAddDevice(group.id, d.serial)}
                                      className="w-full flex items-center gap-3 px-3 py-2 hover:bg-gray-800/50 text-left"
                                    >
                                      <div className={`w-2 h-2 rounded-full ${d.status === 'connected' ? 'bg-green-500' : 'bg-gray-600'}`} />
                                      <div>
                                        <div className="text-sm text-white">{d.nickname || d.model || d.serial}</div>
                                        <div className="text-[10px] text-gray-500">{d.serial}</div>
                                      </div>
                                    </button>
                                  ))}
                                </div>
                              );
                            })()}
                          </div>}

                          {/* Device list */}
                          {members.length === 0 ? (
                            <p className="text-xs text-gray-500">No devices in this group yet.</p>
                          ) : (
                            <div className="space-y-1">
                              {members.map(serial => {
                                const info = getDeviceInfo(serial);
                                return (
                                  <div key={serial} className="flex items-center justify-between px-3 py-2.5 rounded-lg hover:bg-gray-800/40 group">
                                    <div className="flex items-center gap-3">
                                      <div className={`w-2 h-2 rounded-full ${info?.status === 'connected' ? 'bg-green-500' : 'bg-gray-600'}`} />
                                      <div>
                                        <div className="text-sm text-white">{info?.nickname || info?.model || serial}</div>
                                        <div className="text-[10px] text-gray-500">{serial}</div>
                                      </div>
                                      {info?.has_session && (
                                        <span className="px-1.5 py-0.5 rounded text-[10px] bg-green-600/20 text-green-400">streaming</span>
                                      )}
                                    </div>
                                    {isAdmin && (
                                      <button
                                        onClick={() => handleRemoveDevice(group.id, serial)}
                                        className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-red-900/20 transition-opacity"
                                      >
                                        <XIcon />
                                      </button>
                                    )}
                                  </div>
                                );
                              })}
                            </div>
                          )}
                        </>
                      )}

                      {activeTab === 'access' && isAdmin && (
                        <>
                          {/* User grants */}
                          <div className="mb-4">
                            <h4 className="text-xs font-medium text-gray-400 mb-2">User Grants</h4>
                            {grants.length === 0 ? (
                              <p className="text-xs text-gray-600">No user grants.</p>
                            ) : (
                              <div className="space-y-1">
                                {grants.map(grant => (
                                  <div key={grant.id} className="flex items-center justify-between px-3 py-2.5 rounded-lg hover:bg-gray-800/40 group">
                                    <div className="flex items-center gap-3">
                                      <div className="text-gray-500">
                                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" /></svg>
                                      </div>
                                      <span className="text-sm text-white">{grant.username}</span>
                                    </div>
                                    <div className="flex items-center gap-2">
                                      <span className={`px-1.5 py-0.5 rounded text-[10px] ${permissionBadgeClass(grant.permission)}`}>{grant.permission}</span>
                                      <button
                                        onClick={() => handleRevokeAccess(group.id, grant.id)}
                                        className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-red-900/20 transition-opacity"
                                      >
                                        <XIcon />
                                      </button>
                                    </div>
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>

                          {/* Team grants */}
                          <div className="mb-4">
                            <h4 className="text-xs font-medium text-gray-400 mb-2">Team Grants</h4>
                            {tGrants.length === 0 ? (
                              <p className="text-xs text-gray-600">No team grants.</p>
                            ) : (
                              <div className="space-y-1">
                                {tGrants.map(grant => (
                                  <div key={grant.id} className="flex items-center justify-between px-3 py-2.5 rounded-lg hover:bg-gray-800/40 group">
                                    <div className="flex items-center gap-3">
                                      <div className="text-gray-500">
                                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z" /></svg>
                                      </div>
                                      <span className="text-sm text-white">{grant.team_name}</span>
                                    </div>
                                    <div className="flex items-center gap-2">
                                      <span className={`px-1.5 py-0.5 rounded text-[10px] ${permissionBadgeClass(grant.permission)}`}>{grant.permission}</span>
                                      <button
                                        onClick={() => handleRevokeTeamAccess(group.id, grant.id)}
                                        className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-red-900/20 transition-opacity"
                                      >
                                        <XIcon />
                                      </button>
                                    </div>
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>

                          {/* Grant new access */}
                          <div className="flex items-center gap-2 pt-2 border-t border-gray-800/50">
                            <select
                              value={grantTarget}
                              onChange={e => { setGrantTarget(e.target.value as 'user' | 'team'); setGrantUserId(''); setGrantTeamId(''); }}
                              className="px-2 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white"
                            >
                              <option value="user">User</option>
                              <option value="team">Team</option>
                            </select>
                            {grantTarget === 'user' ? (
                              <select
                                value={grantUserId}
                                onChange={e => setGrantUserId(e.target.value)}
                                className="flex-1 px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white"
                              >
                                <option value="">Select user...</option>
                                {users.filter(u => u.role !== 'admin').map(u => (
                                  <option key={u.id} value={u.id}>{u.display_name || u.username}</option>
                                ))}
                              </select>
                            ) : (
                              <select
                                value={grantTeamId}
                                onChange={e => setGrantTeamId(e.target.value)}
                                className="flex-1 px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white"
                              >
                                <option value="">Select team...</option>
                                {teams.map(t => (
                                  <option key={t.id} value={t.id}>{t.name}</option>
                                ))}
                              </select>
                            )}
                            <select
                              value={grantPermission}
                              onChange={e => setGrantPermission(e.target.value)}
                              className="px-2 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white"
                            >
                              <option value="view">view</option>
                              <option value="control">control</option>
                              <option value="manage">manage</option>
                            </select>
                            <button
                              onClick={() => handleGrantAccess(group.id)}
                              disabled={grantTarget === 'user' ? !grantUserId : !grantTeamId}
                              className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50 whitespace-nowrap"
                            >
                              Grant
                            </button>
                          </div>
                        </>
                      )}

                      {activeTab === 'settings' && isAdmin && (
                        <div className="space-y-4">
                          <div>
                            <label className="block text-xs text-gray-400 mb-1">Group Name</label>
                            <input
                              type="text"
                              value={editName}
                              onChange={(e) => setEditName(e.target.value)}
                              className="w-full px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white focus:outline-none focus:border-brand-600"
                            />
                          </div>
                          <div>
                            <label className="block text-xs text-gray-400 mb-1">Description</label>
                            <input
                              type="text"
                              value={editDesc}
                              onChange={(e) => setEditDesc(e.target.value)}
                              className="w-full px-3 py-2 bg-gray-950 border border-gray-800 rounded-lg text-xs text-white focus:outline-none focus:border-brand-600"
                            />
                          </div>
                          <div>
                            <label className="block text-xs text-gray-400 mb-1">Color</label>
                            <div className="flex items-center gap-2">
                              {COLOR_OPTIONS.map(c => (
                                <button
                                  key={c}
                                  onClick={() => setEditColor(c)}
                                  className={`w-7 h-7 rounded-full border-2 ${editColor === c ? 'border-white' : 'border-transparent'}`}
                                  style={{ backgroundColor: c }}
                                />
                              ))}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <button
                              onClick={() => handleUpdateSettings(group.id)}
                              className="px-3 py-2 text-xs bg-brand-600 hover:bg-brand-700 text-white rounded-lg"
                            >
                              Save Changes
                            </button>
                            {settingsSuccess && <span className="text-xs text-green-400">{settingsSuccess}</span>}
                            {settingsError && <span className="text-xs text-red-400">{settingsError}</span>}
                          </div>

                          <div className="pt-4 border-t border-gray-800">
                            {!confirmDelete ? (
                              <button
                                onClick={() => setConfirmDelete(true)}
                                className="px-3 py-2 text-xs bg-red-600/20 hover:bg-red-600/30 text-red-400 rounded-lg"
                              >
                                Delete Group
                              </button>
                            ) : (
                              <div className="flex items-center gap-2">
                                <span className="text-xs text-red-400">Are you sure?</span>
                                <button
                                  onClick={() => handleDelete(group.id)}
                                  className="px-3 py-2 text-xs bg-red-600 hover:bg-red-700 text-white rounded-lg"
                                >
                                  Yes, Delete
                                </button>
                                <button
                                  onClick={() => setConfirmDelete(false)}
                                  className="px-3 py-2 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg"
                                >
                                  Cancel
                                </button>
                              </div>
                            )}
                          </div>
                        </div>
                      )}
                    </div>
                  </>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </>
  );
}
