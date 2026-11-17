'use client';

import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import { Plus, Search, Settings2, Trash2, UserPlus, UsersRound, X } from 'lucide-react';
import { getToken, isAdmin } from '@/lib/auth';
import {
  listUserGroups, createUserGroup, updateUserGroup, deleteUserGroup,
  listUserGroupMembers, addUserGroupMember, removeUserGroupMember,
  listUserGroupAccess, grantUserGroupAccess, revokeUserGroupAccess,
  listUsers, listGroups, listDevices,
  type UserGroup, type UserGroupMember, type UserGroupAccessGrant,
  type UserInfo, type DeviceGroup, type DeviceInfo,
} from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { displayName } from '@/lib/dashboard-filter';
import { Banner, Button, ConfirmDialog, Dialog, EmptyState, Field, FormError, PageHeader, useToast } from '@/components/ui';
import { DataTable } from '@/components/admin/data-table';
import { Monogram, RoleChip } from '@/components/admin/chips';
import { GrantsTable, grantTarget } from '@/components/admin/grants-table';
import { PageBody } from '@/components/admin/page-body';
import { Tabs, tabPanelProps, useTabIds } from '@/components/admin/tabs';

type Tab = 'members' | 'access' | 'settings';

const PERMISSION_HINT = 'View watches. Control also uses the phone. Manage also edits it.';

function memberName(m: { display_name: string; username: string }): string {
  return m.display_name || m.username;
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

export default function UserGroupsPage() {
  const router = useRouter();
  const toast = useToast();
  const [groups, setGroups] = useState<UserGroup[] | null>(null);
  const [loadError, setLoadError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<UserGroup | null>(null);

  // The dialog for one team.
  const [detailId, setDetailId] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>('members');
  const [members, setMembers] = useState<UserGroupMember[]>([]);
  const [grants, setGrants] = useState<UserGroupAccessGrant[]>([]);
  const [allUsers, setAllUsers] = useState<UserInfo[]>([]);
  const [allDeviceGroups, setAllDeviceGroups] = useState<DeviceGroup[]>([]);
  const [allDevices, setAllDevices] = useState<DeviceInfo[]>([]);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState('');
  const [revokeTarget, setRevokeTarget] = useState<UserGroupAccessGrant | null>(null);
  const detailReq = useRef(0);
  const tabIds = useTabIds();

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
  }, [router]);

  const fetchGroups = useCallback(async () => {
    setRefreshing(true);
    try {
      setGroups(await listUserGroups());
      setLoadError('');
    } catch (e) {
      setLoadError(errorMessage(e, 'load teams'));
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => { fetchGroups(); }, [fetchGroups]);

  const loadDetails = useCallback(async (id: string) => {
    const req = ++detailReq.current;
    setDetailLoading(true);
    setDetailError('');
    try {
      const [m, g, u, dg, d] = await Promise.all([
        listUserGroupMembers(id),
        listUserGroupAccess(id),
        listUsers(),
        listGroups(),
        listDevices(),
      ]);
      if (req !== detailReq.current) return;
      setMembers(m);
      setGrants(g);
      setAllUsers(u);
      setAllDeviceGroups(dg);
      setAllDevices(d);
    } catch (e) {
      if (req !== detailReq.current) return;
      setDetailError(errorMessage(e, 'load the team'));
    } finally {
      if (req === detailReq.current) setDetailLoading(false);
    }
  }, []);

  const openDetail = (g: UserGroup, startTab: Tab = 'members') => {
    setMembers([]);
    setGrants([]);
    setTab(startTab);
    setDetailId(g.id);
    loadDetails(g.id);
  };

  const detailGroup = groups?.find((g) => g.id === detailId) ?? null;
  const deviceNames = Object.fromEntries(allDevices.map((d) => [d.serial, displayName(d)]));

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Teams"
        sub={
          <span>
            Groups of users that get access to phones together.
            {groups && ` ${plural(groups.length, 'team', 'teams')}.`}
          </span>
        }
        actions={
          <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
            New team
          </Button>
        }
      />

      <PageBody>
        {loadError && (
          <Banner
            tone="danger"
            actions={
              <Button size="sm" onClick={fetchGroups} disabled={refreshing}>
                Retry
              </Button>
            }
          >
            {loadError}
          </Banner>
        )}

        {groups === null && !loadError && (
          <div role="status" className="border border-vx-hairline bg-vx-surface px-4 py-10 text-center text-sm text-vx-fg-muted">
            Loading teams…
          </div>
        )}

        {groups && groups.length === 0 && (
          <EmptyState
            icon={<UsersRound />}
            title="No teams yet"
            action={
              <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
                New team
              </Button>
            }
          >
            Create a team, add users to it, then grant it phones. Everyone in the team gets that access.
          </EmptyState>
        )}

        {groups && groups.length > 0 && (
          <DataTable
            caption="Teams"
            rows={groups}
            rowKey={(g) => g.id}
            refreshing={refreshing}
            columns={[
              {
                key: 'team',
                header: 'Team',
                primary: true,
                cell: (g) => (
                  <div className="flex items-center gap-3">
                    <Monogram name={g.name} />
                    <div className="min-w-0">
                      <button
                        type="button"
                        onClick={() => openDetail(g)}
                        className="-mx-1 block max-w-full truncate rounded-sm px-1 text-left font-semibold text-vx-fg hover:text-vx-accent"
                      >
                        {g.name}
                      </button>
                      {g.description && <div className="text-[13px] text-vx-fg-muted">{g.description}</div>}
                    </div>
                  </div>
                ),
              },
              {
                key: 'members',
                header: 'Members',
                cell: (g) => <span className="font-mono text-[13px] text-vx-fg-muted">{g.member_count}</span>,
              },
            ]}
            actions={(g) => (
              <>
                <Button size="md" className="max-sm:h-touch" icon={<Settings2 size={14} />} onClick={() => openDetail(g)}>
                  Manage
                </Button>
                <Button
                  variant="quiet"
                  iconOnly
                  aria-label={`Delete ${g.name}`}
                  icon={<Trash2 size={16} />}
                  className="max-sm:h-touch max-sm:w-touch"
                  onClick={() => setDeleteTarget(g)}
                />
              </>
            )}
          />
        )}
      </PageBody>

      <CreateTeamDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreate={async (name, description) => {
          await createUserGroup(name, description);
          toast.success(`Created ${name}.`);
          fetchGroups();
        }}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title={deleteTarget ? `Delete ${deleteTarget.name}?` : ''}
        confirmLabel="Delete"
        consequence={
          deleteTarget && (
            <>
              {plural(deleteTarget.member_count, 'member', 'members')} lose the access granted through {deleteTarget.name}.
              The users are not deleted. This can&apos;t be undone.
            </>
          )
        }
        formatError={(e) => errorMessage(e, 'delete the team')}
        onConfirm={async () => {
          if (!deleteTarget) return;
          await deleteUserGroup(deleteTarget.id);
          toast.success(`Deleted ${deleteTarget.name}.`);
          if (detailId === deleteTarget.id) setDetailId(null);
          fetchGroups();
        }}
      />

      <Dialog
        open={!!detailGroup}
        onClose={() => setDetailId(null)}
        size="lg"
        title={detailGroup?.name ?? ''}
        description={detailGroup?.description || undefined}
        footer={<Button onClick={() => setDetailId(null)}>Close</Button>}
      >
        {detailGroup && (
          <div className="space-y-4">
            <Tabs
              label="Team"
              ids={tabIds}
              value={tab}
              onChange={setTab}
              tabs={[
                { key: 'members', label: 'Members' },
                { key: 'access', label: 'Device access' },
                { key: 'settings', label: 'Settings' },
              ]}
            />
            {detailError && <FormError>{detailError}</FormError>}

            {tab === 'members' && (
              <div {...tabPanelProps(tabIds, 'members')} className="space-y-4" aria-busy={detailLoading || undefined}>
                <MembersPanel
                  group={detailGroup}
                  members={members}
                  allUsers={allUsers}
                  loading={detailLoading}
                  onChanged={() => {
                    loadDetails(detailGroup.id);
                    fetchGroups();
                  }}
                />
              </div>
            )}

            {tab === 'access' && (
              <div {...tabPanelProps(tabIds, 'access')} className="space-y-6" aria-busy={detailLoading || undefined}>
                {detailLoading && grants.length === 0 ? (
                  <p role="status" className="text-sm text-vx-fg-muted">
                    Loading access…
                  </p>
                ) : grants.length === 0 ? (
                  <p className="text-sm text-vx-fg-muted">No access yet. Grant a phone or a group of phones below.</p>
                ) : (
                  <GrantsTable grants={grants} deviceNames={deviceNames} onRevoke={setRevokeTarget} />
                )}
                <GrantForm
                  group={detailGroup}
                  devices={allDevices}
                  deviceGroups={allDeviceGroups}
                  onGranted={(what, permission) => {
                    toast.success(`Granted ${permission} on ${what} to ${detailGroup.name}.`);
                    loadDetails(detailGroup.id);
                  }}
                />
              </div>
            )}

            {tab === 'settings' && (
              <div {...tabPanelProps(tabIds, 'settings')} className="space-y-6">
                <SettingsForm
                  group={detailGroup}
                  onSaved={() => {
                    toast.success('Team saved.');
                    fetchGroups();
                  }}
                />
                <div className="border-t border-vx-hairline pt-4">
                  <h3 className="vx-label mb-2">Delete team</h3>
                  <p className="mb-3 text-sm text-vx-fg-muted">
                    Members lose the access granted through this team. The users are not deleted.
                  </p>
                  <Button variant="danger" icon={<Trash2 size={14} />} onClick={() => setDeleteTarget(detailGroup)}>
                    Delete team
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={!!revokeTarget}
        onClose={() => setRevokeTarget(null)}
        title={
          revokeTarget && detailGroup
            ? `Revoke ${detailGroup.name}'s access to ${grantTarget(revokeTarget, deviceNames)}?`
            : ''
        }
        confirmLabel="Revoke"
        consequence={
          revokeTarget && detailGroup && (
            <>
              Members of {detailGroup.name} can&apos;t open {grantTarget(revokeTarget, deviceNames)} through this team any
              more. Access they have another way is not affected.
            </>
          )
        }
        formatError={(e) => errorMessage(e, 'revoke access')}
        onConfirm={async () => {
          if (!revokeTarget || !detailGroup) return;
          await revokeUserGroupAccess(detailGroup.id, revokeTarget.id);
          toast.success(`Revoked access to ${grantTarget(revokeTarget, deviceNames)}.`);
          loadDetails(detailGroup.id);
        }}
      />
    </>
  );
}

function CreateTeamDialog({
  open,
  onClose,
  onCreate,
}: {
  open: boolean;
  onClose: () => void;
  onCreate: (name: string, description: string) => Promise<void>;
}) {
  const formId = useId();
  const nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setName('');
    setDescription('');
    setError('');
    nameRef.current?.focus();
  }, [open]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      await onCreate(name.trim(), description.trim());
      onClose();
    } catch (err) {
      setError(errorMessage(err, 'create the team'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      busy={busy}
      title="New team"
      description="Add users and grant phones once you've created it."
      footer={
        <>
          <Button variant="quiet" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" form={formId} variant="primary" disabled={busy || !name.trim()}>
            {busy ? 'Creating…' : 'Create team'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="space-y-4">
        <Field label="Team name" hint="Names must be unique.">
          <input ref={nameRef} className="vx-input" type="text" value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field label="Description (optional)">
          <input className="vx-input" type="text" value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <FormError>{error}</FormError>
      </form>
    </Dialog>
  );
}

function MembersPanel({
  group,
  members,
  allUsers,
  loading,
  onChanged,
}: {
  group: UserGroup;
  members: UserGroupMember[];
  allUsers: UserInfo[];
  loading: boolean;
  onChanged: () => void;
}) {
  const toast = useToast();
  const [query, setQuery] = useState('');
  const [working, setWorking] = useState<string | null>(null);

  const q = query.trim().toLowerCase();
  const matches = q
    ? allUsers
        .filter((u) => !members.some((m) => m.user_id === u.id))
        .filter((u) => u.username.toLowerCase().includes(q) || (u.display_name || '').toLowerCase().includes(q))
        .slice(0, 8)
    : [];

  const add = async (u: UserInfo) => {
    setWorking(u.id);
    try {
      await addUserGroupMember(group.id, u.id);
      toast.success(`Added ${memberName(u)} to ${group.name}.`);
      setQuery('');
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e, 'add the member'));
    } finally {
      setWorking(null);
    }
  };

  const remove = async (m: UserGroupMember) => {
    setWorking(m.user_id);
    try {
      await removeUserGroupMember(group.id, m.user_id);
      toast.success(`Removed ${memberName(m)} from ${group.name}.`);
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e, 'remove the member'));
    } finally {
      setWorking(null);
    }
  };

  return (
    <>
      <div className="space-y-2">
        <Field label="Add a member" hint="Search by name or username.">
          <input
            className="vx-input"
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            autoComplete="off"
          />
        </Field>
        {q && matches.length === 0 && (
          <p role="status" className="text-sm text-vx-fg-muted">
            <Search size={14} className="mr-1.5 inline align-[-2px]" aria-hidden />
            No users to add match &quot;{query.trim()}&quot;.
          </p>
        )}
        {matches.length > 0 && (
          <ul className="grid gap-px border border-vx-hairline bg-vx-hairline" aria-label="Users you can add">
            {matches.map((u) => (
              <li key={u.id} className="flex items-center gap-3 bg-vx-surface px-3 py-2">
                <Monogram name={memberName(u)} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm">{memberName(u)}</div>
                  <div className="truncate font-mono text-xs text-vx-fg-label">@{u.username}</div>
                </div>
                <RoleChip role={u.role} />
                <Button
                  size="md"
                  className="max-sm:h-touch"
                  icon={<UserPlus size={14} />}
                  aria-label={`Add ${memberName(u)}`}
                  disabled={working === u.id}
                  onClick={() => add(u)}
                >
                  Add
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {loading && members.length === 0 ? (
        <p role="status" className="text-sm text-vx-fg-muted">
          Loading members…
        </p>
      ) : members.length === 0 ? (
        <p className="text-sm text-vx-fg-muted">No members yet. Search above to add users.</p>
      ) : (
        <DataTable
          caption="Members"
          rows={members}
          rowKey={(m) => m.user_id}
          columns={[
            {
              key: 'member',
              header: 'Member',
              primary: true,
              cell: (m) => (
                <div className="flex items-center gap-3">
                  <Monogram name={memberName(m)} />
                  <div className="min-w-0">
                    <div className="truncate text-sm">{memberName(m)}</div>
                    <div className="truncate font-mono text-xs text-vx-fg-label">@{m.username}</div>
                  </div>
                </div>
              ),
            },
            { key: 'role', header: 'Role', cell: (m) => <RoleChip role={m.role} /> },
          ]}
          actions={(m) => (
            <Button
              variant="quiet"
              iconOnly
              aria-label={`Remove ${memberName(m)} from ${group.name}`}
              icon={<X size={16} />}
              className="max-sm:h-touch max-sm:w-touch"
              disabled={working === m.user_id}
              onClick={() => remove(m)}
            />
          )}
        />
      )}
    </>
  );
}

function GrantForm({
  group,
  devices,
  deviceGroups,
  onGranted,
}: {
  group: UserGroup;
  devices: DeviceInfo[];
  deviceGroups: DeviceGroup[];
  onGranted: (what: string, permission: string) => void;
}) {
  const [target, setTarget] = useState<'device' | 'group'>('device');
  const [serial, setSerial] = useState('');
  const [groupId, setGroupId] = useState('');
  const [permission, setPermission] = useState('view');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const picked = target === 'device' ? serial : groupId;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !picked) return;
    const body: { device_serial?: string; group_id?: string; permission: string } = { permission };
    let what = '';
    if (target === 'device') {
      body.device_serial = serial;
      const d = devices.find((x) => x.serial === serial);
      what = d ? displayName(d) : serial;
    } else {
      body.group_id = groupId;
      what = deviceGroups.find((x) => x.id === groupId)?.name ?? 'the group';
    }
    setBusy(true);
    setError('');
    try {
      await grantUserGroupAccess(group.id, body);
      setSerial('');
      setGroupId('');
      setPermission('view');
      onGranted(what, permission);
    } catch (err) {
      setError(errorMessage(err, 'grant access'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4 border-t border-vx-hairline pt-4" aria-label="Grant access">
      <h3 className="vx-label">Grant access</h3>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Grant access to">
          <select
            className="vx-input"
            value={target}
            onChange={(e) => setTarget(e.target.value as 'device' | 'group')}
          >
            <option value="device">A phone</option>
            <option value="group">A group of phones</option>
          </select>
        </Field>
        {target === 'device' ? (
          <Field label="Phone">
            <select className="vx-input" value={serial} onChange={(e) => setSerial(e.target.value)}>
              <option value="">Choose a phone</option>
              {devices.map((d) => (
                <option key={d.serial} value={d.serial}>
                  {displayName(d)}
                </option>
              ))}
            </select>
          </Field>
        ) : (
          <Field label="Group of phones">
            <select className="vx-input" value={groupId} onChange={(e) => setGroupId(e.target.value)}>
              <option value="">Choose a group</option>
              {deviceGroups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          </Field>
        )}
        <Field label="Permission" hint={PERMISSION_HINT} className="flex flex-col gap-2 sm:col-span-2">
          <select className="vx-input" value={permission} onChange={(e) => setPermission(e.target.value)}>
            <option value="view">view</option>
            <option value="control">control</option>
            <option value="manage">manage</option>
          </select>
        </Field>
      </div>
      <FormError>{error}</FormError>
      <Button type="submit" variant="primary" disabled={busy || !picked}>
        {busy ? 'Granting…' : 'Grant access'}
      </Button>
    </form>
  );
}

function SettingsForm({ group, onSaved }: { group: UserGroup; onSaved: () => void }) {
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description || '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      await updateUserGroup(group.id, { name: name.trim(), description });
      onSaved();
    } catch (err) {
      setError(errorMessage(err, 'save the team'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4" aria-label="Team settings">
      <Field label="Team name">
        <input className="vx-input" type="text" value={name} onChange={(e) => setName(e.target.value)} required />
      </Field>
      <Field label="Description (optional)">
        <input className="vx-input" type="text" value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <FormError>{error}</FormError>
      <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
        {busy ? 'Saving…' : 'Save changes'}
      </Button>
    </form>
  );
}
