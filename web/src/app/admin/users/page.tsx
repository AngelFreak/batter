'use client';

import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import { KeyRound, MoreHorizontal, Plus, Trash2, UserCheck, UserX, Users } from 'lucide-react';
import { getToken, isAdmin } from '@/lib/auth';
import {
  listUsers, createUser, updateUser, deleteUser,
  listUserAccess, revokeUserAccess, resetPassword,
  listUserGroups, listUserGroupMembers, listDevices,
  type UserInfo, type UserAccessGrant,
} from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { useUser } from '@/lib/use-user';
import { displayName } from '@/lib/dashboard-filter';
import {
  Banner, Button, ConfirmDialog, Dialog, EmptyState, Field, FormError, Menu, PageHeader, StatusGlyph, useToast,
  type MenuItem,
} from '@/components/ui';
import { DataTable } from '@/components/admin/data-table';
import { Monogram, RoleChip } from '@/components/admin/chips';
import { GrantsTable, grantTarget } from '@/components/admin/grants-table';
import { PageBody } from '@/components/admin/page-body';
import { ROLES, ROLE_CAN, ROLE_LABEL, isRole, roleArticle, type Role } from '@/components/admin/roles';

// The backend hashes with bcrypt and refuses anything under 8 characters
// (internal/auth/password.go). It sets no other rule.
const MIN_PASSWORD = 8;
const PASSWORD_RULE = `At least ${MIN_PASSWORD} characters.`;

interface TeamMembership {
  teamId: string;
  teamName: string;
}

function nameOf(u: UserInfo): string {
  return u.display_name || u.username;
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString();
}

export default function AdminUsersPage() {
  const router = useRouter();
  const toast = useToast();
  const { user: current } = useUser();
  const me = current?.id ?? null;
  const [users, setUsers] = useState<UserInfo[] | null>(null);
  const [loadError, setLoadError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [deviceNames, setDeviceNames] = useState<Record<string, string>>({});

  const [creating, setCreating] = useState(false);
  const [roleTarget, setRoleTarget] = useState<{ user: UserInfo; role: Role } | null>(null);
  const [disableTarget, setDisableTarget] = useState<UserInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<UserInfo | null>(null);
  const [resetTarget, setResetTarget] = useState<UserInfo | null>(null);

  // The access dialog for one user.
  const [detailId, setDetailId] = useState<string | null>(null);
  const [grants, setGrants] = useState<UserAccessGrant[]>([]);
  const [teams, setTeams] = useState<TeamMembership[]>([]);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState('');
  const [revokeTarget, setRevokeTarget] = useState<UserAccessGrant | null>(null);
  const detailReq = useRef(0);

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
  }, [router]);

  const fetchUsers = useCallback(async () => {
    setRefreshing(true);
    try {
      setUsers(await listUsers());
      setLoadError('');
    } catch (e) {
      setLoadError(errorMessage(e, 'load users'));
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => { fetchUsers(); }, [fetchUsers]);

  // Phone names for the access list. Without them the list falls back to serials.
  useEffect(() => {
    listDevices()
      .then((ds) => setDeviceNames(Object.fromEntries(ds.map((d) => [d.serial, displayName(d)]))))
      .catch(() => { /* names are a nicety; serials still show */ });
  }, []);

  const loadDetails = useCallback(async (userId: string) => {
    const req = ++detailReq.current;
    setDetailLoading(true);
    setDetailError('');
    try {
      const [g, t] = await Promise.all([listUserAccess(userId), loadTeamMemberships(userId)]);
      if (req !== detailReq.current) return;
      setGrants(g);
      setTeams(t);
    } catch (e) {
      if (req !== detailReq.current) return;
      setDetailError(errorMessage(e, 'load access'));
    } finally {
      if (req === detailReq.current) setDetailLoading(false);
    }
  }, []);

  const openDetail = (u: UserInfo) => {
    setGrants([]);
    setTeams([]);
    setDetailId(u.id);
    loadDetails(u.id);
  };

  const detailUser = users?.find((u) => u.id === detailId) ?? null;

  const enable = async (u: UserInfo) => {
    try {
      await updateUser(u.id, { is_active: true });
      toast.success(`${nameOf(u)} enabled.`);
      fetchUsers();
    } catch (e) {
      toast.error(errorMessage(e, 'enable the user'));
    }
  };

  const menuItems = (u: UserInfo): (MenuItem | 'separator')[] => {
    const isMe = u.id === me;
    const items: (MenuItem | 'separator')[] = [
      ...ROLES.filter((r) => r !== u.role).map<MenuItem>((r) => ({
        label: `Make ${r}`,
        icon: <Users />,
        onSelect: () => setRoleTarget({ user: u, role: r }),
        // Demoting yourself could leave no admin to undo it.
        disabled: isMe,
        disabledReason: "You can't change your own role.",
      })),
      u.is_active
        ? {
            label: 'Disable user',
            icon: <UserX />,
            onSelect: () => setDisableTarget(u),
            disabled: isMe,
            disabledReason: "You can't disable your own account.",
          }
        : { label: 'Enable user', icon: <UserCheck />, onSelect: () => enable(u) },
      { label: 'Reset password', icon: <KeyRound />, onSelect: () => setResetTarget(u) },
      'separator',
      {
        label: 'Delete user',
        icon: <Trash2 />,
        danger: true,
        onSelect: () => setDeleteTarget(u),
        disabled: isMe,
        disabledReason: "You can't delete your own account.",
      },
    ];
    return items;
  };

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Users"
        sub={
          <span>
            Who can sign in to Batter, and what each person can do.
            {users && ` ${users.length} ${users.length === 1 ? 'user' : 'users'}.`}
          </span>
        }
        actions={
          <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
            New user
          </Button>
        }
      />

      <PageBody>
        {loadError && (
          <Banner
            tone="danger"
            actions={
              <Button size="sm" onClick={fetchUsers} disabled={refreshing}>
                Retry
              </Button>
            }
          >
            {loadError}
          </Banner>
        )}

        {users === null && !loadError && (
          <div role="status" className="border border-vx-hairline bg-vx-surface px-4 py-10 text-center text-sm text-vx-fg-muted">
            Loading users…
          </div>
        )}

        {users && users.length === 0 && (
          <EmptyState
            icon={<Users />}
            title="No users yet"
            action={
              <Button variant="primary" icon={<Plus size={16} />} onClick={() => setCreating(true)}>
                New user
              </Button>
            }
          >
            Add a user to let someone sign in.
          </EmptyState>
        )}

        {users && users.length > 0 && (
          <DataTable
            caption="Users"
            rows={users}
            rowKey={(u) => u.id}
            refreshing={refreshing}
            columns={[
              {
                key: 'user',
                header: 'User',
                primary: true,
                cell: (u) => (
                  <div className="flex items-center gap-3">
                    <Monogram name={nameOf(u)} />
                    <div className="min-w-0 flex-1">
                      <button
                        type="button"
                        onClick={() => openDetail(u)}
                        className="-mx-1 block max-w-full truncate rounded-sm px-1 text-left font-semibold text-vx-fg hover:text-vx-accent"
                      >
                        {nameOf(u)}
                      </button>
                      <div className="truncate font-mono text-xs text-vx-fg-label">@{u.username}</div>
                    </div>
                  </div>
                ),
              },
              { key: 'role', header: 'Role', cell: (u) => <RoleChip role={u.role} /> },
              {
                key: 'status',
                header: 'Status',
                cell: (u) => (
                  <span
                    className={`inline-flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-wide ${
                      u.is_active ? 'text-vx-fg-muted' : 'text-vx-fg-label'
                    }`}
                  >
                    <StatusGlyph icon={u.is_active ? 'dot' : 'slash'} />
                    {u.is_active ? 'Active' : 'Disabled'}
                  </span>
                ),
              },
              {
                key: 'login',
                header: 'Last sign-in',
                cell: (u) =>
                  u.last_login_at ? (
                    <span className="text-[13px] text-vx-fg-muted">{formatDate(u.last_login_at)}</span>
                  ) : (
                    <span className="text-[13px] text-vx-fg-label">Never</span>
                  ),
              },
            ]}
            actions={(u) => (
              <>
                <Button size="md" className="max-sm:h-touch" onClick={() => openDetail(u)}>
                  Access
                </Button>
                <Menu
                  items={menuItems(u)}
                  trigger={(p) => (
                    <Button
                      {...p}
                      variant="quiet"
                      iconOnly
                      aria-label={`More actions for ${nameOf(u)}`}
                      icon={<MoreHorizontal size={16} />}
                      className="max-sm:h-touch max-sm:w-touch"
                    />
                  )}
                />
              </>
            )}
          />
        )}
      </PageBody>

      <CreateUserDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreate={async (username, password, role) => {
          await createUser(username, password, role);
          toast.success(`Created ${username}.`);
          fetchUsers();
        }}
      />

      <ConfirmDialog
        open={!!roleTarget}
        onClose={() => setRoleTarget(null)}
        title={roleTarget ? `Make ${nameOf(roleTarget.user)} ${roleArticle(roleTarget.role)} ${roleTarget.role}?` : ''}
        tone="primary"
        confirmLabel={roleTarget ? `Make ${roleTarget.role}` : 'Confirm'}
        consequence={
          roleTarget && (
            <>
              {roleArticle(roleTarget.role) === 'an' ? 'An' : 'A'} {roleTarget.role} can {ROLE_CAN[roleTarget.role]}.
            </>
          )
        }
        formatError={(e) => errorMessage(e, 'change the role')}
        onConfirm={async () => {
          if (!roleTarget) return;
          await updateUser(roleTarget.user.id, { role: roleTarget.role });
          toast.success(`${nameOf(roleTarget.user)} is now ${roleArticle(roleTarget.role)} ${roleTarget.role}.`);
          fetchUsers();
        }}
      />

      {/* The backend blocks a disabled account at sign-in and at token refresh
          (handlers/auth.go); a token already issued runs until it expires. */}
      <ConfirmDialog
        open={!!disableTarget}
        onClose={() => setDisableTarget(null)}
        title={disableTarget ? `Disable ${nameOf(disableTarget)}?` : ''}
        confirmLabel="Disable"
        consequence={
          disableTarget && (
            <>
              {nameOf(disableTarget)} can&apos;t sign in until you enable the account again. A sign-in that is already
              open ends when its token expires.
            </>
          )
        }
        formatError={(e) => errorMessage(e, 'disable the user')}
        onConfirm={async () => {
          if (!disableTarget) return;
          await updateUser(disableTarget.id, { is_active: false });
          toast.success(`${nameOf(disableTarget)} disabled.`);
          fetchUsers();
        }}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title={deleteTarget ? `Delete ${nameOf(deleteTarget)}?` : ''}
        confirmLabel="Delete"
        consequence={<>This can&apos;t be undone.</>}
        formatError={(e) => errorMessage(e, 'delete the user')}
        onConfirm={async () => {
          if (!deleteTarget) return;
          await deleteUser(deleteTarget.id);
          toast.success(`Deleted ${nameOf(deleteTarget)}.`);
          if (detailId === deleteTarget.id) setDetailId(null);
          fetchUsers();
        }}
      />

      <ResetPasswordDialog
        user={resetTarget}
        onClose={() => setResetTarget(null)}
        onReset={async (u, password) => {
          await resetPassword(u.id, password);
          toast.success(`Password reset for ${nameOf(u)}.`);
        }}
      />

      <Dialog
        open={!!detailUser}
        onClose={() => setDetailId(null)}
        size="lg"
        title={detailUser ? nameOf(detailUser) : ''}
        description={detailUser && <span className="font-mono text-[13px]">@{detailUser.username}</span>}
        footer={<Button onClick={() => setDetailId(null)}>Close</Button>}
      >
        {detailUser && (
          <div className="space-y-6">
            <dl className="grid gap-px border border-vx-hairline bg-vx-hairline sm:grid-cols-4">
              <Info label="Role">
                <RoleChip role={detailUser.role} />
              </Info>
              <Info label="Status">{detailUser.is_active ? 'Active' : 'Disabled'}</Info>
              <Info label="Last sign-in">
                {detailUser.last_login_at ? new Date(detailUser.last_login_at).toLocaleString() : 'Never'}
              </Info>
              <Info label="Created">{formatDate(detailUser.created_at)}</Info>
            </dl>

            {detailError && (
              <FormError>{detailError}</FormError>
            )}

            <section aria-labelledby="direct-access" className="space-y-3" aria-busy={detailLoading || undefined}>
              <h3 id="direct-access" className="vx-label">
                Direct access
              </h3>
              {detailLoading && grants.length === 0 ? (
                <p role="status" className="text-sm text-vx-fg-muted">
                  Loading access…
                </p>
              ) : grants.length === 0 ? (
                <p className="text-sm text-vx-fg-muted">No direct access. Anything they reach comes from a team.</p>
              ) : (
                <GrantsTable grants={grants} deviceNames={deviceNames} onRevoke={setRevokeTarget} />
              )}
            </section>

            <section aria-labelledby="teams" className="space-y-3">
              <h3 id="teams" className="vx-label">
                Teams
              </h3>
              {detailLoading && teams.length === 0 ? (
                <p role="status" className="text-sm text-vx-fg-muted">
                  Loading teams…
                </p>
              ) : teams.length === 0 ? (
                <p className="text-sm text-vx-fg-muted">Not in any team.</p>
              ) : (
                <ul className="grid gap-px border border-vx-hairline bg-vx-hairline">
                  {teams.map((t) => (
                    <li key={t.teamId} className="flex items-center gap-3 bg-vx-surface px-4 py-3 text-sm">
                      <Users size={16} className="shrink-0 text-vx-fg-label" aria-hidden />
                      {t.teamName}
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={!!revokeTarget}
        onClose={() => setRevokeTarget(null)}
        title={
          revokeTarget && detailUser
            ? `Revoke ${nameOf(detailUser)}'s access to ${grantTarget(revokeTarget, deviceNames)}?`
            : ''
        }
        confirmLabel="Revoke"
        consequence={
          revokeTarget && detailUser && (
            <>
              {nameOf(detailUser)} can&apos;t open {grantTarget(revokeTarget, deviceNames)} again through this
              grant. Access from a team is not affected.
            </>
          )
        }
        formatError={(e) => errorMessage(e, 'revoke access')}
        onConfirm={async () => {
          if (!revokeTarget || !detailUser) return;
          await revokeUserAccess(detailUser.id, revokeTarget.id);
          toast.success(`Revoked access to ${grantTarget(revokeTarget, deviceNames)}.`);
          loadDetails(detailUser.id);
        }}
      />
    </>
  );
}

// One request per team, as before: the API has no "teams of this user" call.
async function loadTeamMemberships(userId: string): Promise<TeamMembership[]> {
  const teams = await listUserGroups();
  const memberships: TeamMembership[] = [];
  for (const team of teams) {
    const members = await listUserGroupMembers(team.id);
    if (members.some((m) => m.user_id === userId)) {
      memberships.push({ teamId: team.id, teamName: team.name });
    }
  }
  return memberships;
}

function Info({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="bg-vx-surface px-4 py-3">
      <dt className="vx-label">{label}</dt>
      <dd className="mt-1 text-sm text-vx-fg">{children}</dd>
    </div>
  );
}

function CreateUserDialog({
  open,
  onClose,
  onCreate,
}: {
  open: boolean;
  onClose: () => void;
  onCreate: (username: string, password: string, role: string) => Promise<void>;
}) {
  const formId = useId();
  const nameRef = useRef<HTMLInputElement>(null);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<Role>('viewer');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [pwError, setPwError] = useState('');

  useEffect(() => {
    if (!open) return;
    setUsername('');
    setPassword('');
    setRole('viewer');
    setError('');
    setPwError('');
    nameRef.current?.focus();
  }, [open]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setError('');
    setPwError('');
    if (password.length < MIN_PASSWORD) {
      setPwError(PASSWORD_RULE);
      return;
    }
    setBusy(true);
    try {
      await onCreate(username.trim(), password, role);
      onClose();
    } catch (err) {
      setError(errorMessage(err, 'create the user'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      busy={busy}
      title="New user"
      description="They sign in with this username and password."
      footer={
        <>
          <Button variant="quiet" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" form={formId} variant="primary" disabled={busy || !username.trim() || !password}>
            {busy ? 'Creating…' : 'Create user'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="space-y-4">
        <Field label="Username">
          <input
            ref={nameRef}
            className="vx-input"
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            required
          />
        </Field>
        <Field label="Password" hint={PASSWORD_RULE} error={pwError}>
          <input
            className="vx-input"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
        </Field>
        <Field label="Role" hint={`${ROLE_LABEL[role]}: ${ROLE_CAN[role]}.`}>
          <select
            className="vx-input"
            value={role}
            onChange={(e) => {
              if (isRole(e.target.value)) setRole(e.target.value);
            }}
          >
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {ROLE_LABEL[r]}
              </option>
            ))}
          </select>
        </Field>
        <FormError>{error}</FormError>
      </form>
    </Dialog>
  );
}

function ResetPasswordDialog({
  user,
  onClose,
  onReset,
}: {
  user: UserInfo | null;
  onClose: () => void;
  onReset: (user: UserInfo, password: string) => Promise<void>;
}) {
  const formId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [pwError, setPwError] = useState('');
  const open = !!user;

  useEffect(() => {
    if (!open) return;
    setPassword('');
    setError('');
    setPwError('');
    inputRef.current?.focus();
  }, [open]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!user || busy) return;
    setError('');
    setPwError('');
    if (password.length < MIN_PASSWORD) {
      setPwError(PASSWORD_RULE);
      return;
    }
    setBusy(true);
    try {
      await onReset(user, password);
      onClose();
    } catch (err) {
      setError(errorMessage(err, 'reset the password'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      busy={busy}
      title={user ? `Reset password for ${nameOf(user)}` : 'Reset password'}
      description="Choose a new password and pass it on to them yourself."
      footer={
        <>
          <Button variant="quiet" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" form={formId} variant="primary" disabled={busy || !password}>
            {busy ? 'Resetting…' : 'Reset password'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="space-y-4">
        <Field label="New password" hint={PASSWORD_RULE} error={pwError}>
          <input
            ref={inputRef}
            className="vx-input"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
        </Field>
        <FormError>{error}</FormError>
      </form>
    </Dialog>
  );
}
