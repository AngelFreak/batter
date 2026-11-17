'use client';

import { useCallback, useEffect, useState } from 'react';
import { User, Users, X } from 'lucide-react';
import {
  listGroupAccess, listGroupTeamAccess, listUsers, listUserGroups,
  grantAccess, grantGroupTeamAccess, revokeAccess, revokeGroupTeamAccess,
  DeviceGroup, GroupAccessGrant, GroupTeamAccessGrant, UserInfo, UserGroup,
} from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { Button, ConfirmDialog, Field, FormError, useToast } from '@/components/ui';
import { PermissionChip } from './permission-chip';

type Revoking = { kind: 'user' | 'team'; id: string; who: string } | null;

/** Admin only. Who may see or control the phones in this group. */
export function GroupAccessTab({ group }: { group: DeviceGroup }) {
  const toast = useToast();
  const [grants, setGrants] = useState<GroupAccessGrant[] | null>(null);
  const [teamGrants, setTeamGrants] = useState<GroupTeamAccessGrant[] | null>(null);
  const [users, setUsers] = useState<UserInfo[]>([]);
  const [teams, setTeams] = useState<UserGroup[]>([]);
  const [loadError, setLoadError] = useState('');

  const [target, setTarget] = useState<'user' | 'team'>('user');
  const [userId, setUserId] = useState('');
  const [teamId, setTeamId] = useState('');
  const [permission, setPermission] = useState('view');
  const [granting, setGranting] = useState(false);
  const [grantError, setGrantError] = useState('');
  const [revoking, setRevoking] = useState<Revoking>(null);

  const load = useCallback(async () => {
    try {
      const [g, tg, u, t] = await Promise.all([
        listGroupAccess(group.id),
        listGroupTeamAccess(group.id),
        listUsers(),
        listUserGroups(),
      ]);
      setGrants(g);
      setTeamGrants(tg);
      setUsers(u);
      setTeams(t);
      setLoadError('');
    } catch (err) {
      setLoadError(errorMessage(err, 'load who has access'));
    }
  }, [group.id]);

  useEffect(() => {
    load();
  }, [load]);

  const grant = async (e: React.FormEvent) => {
    e.preventDefault();
    setGrantError('');
    if (target === 'user' ? !userId : !teamId) {
      setGrantError(target === 'user' ? 'Choose a user.' : 'Choose a team.');
      return;
    }
    setGranting(true);
    try {
      let who: string;
      if (target === 'user') {
        const u = users.find(x => x.id === userId);
        who = u ? u.display_name || u.username : 'the user';
        await grantAccess(userId, { group_id: group.id, permission });
        setUserId('');
      } else {
        who = teams.find(x => x.id === teamId)?.name ?? 'the team';
        await grantGroupTeamAccess(group.id, teamId, permission);
        setTeamId('');
      }
      toast.success(`${who} can now ${permission} ${group.name}.`);
      setPermission('view');
      await load();
    } catch (err) {
      setGrantError(errorMessage(err, 'grant access'));
    } finally {
      setGranting(false);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      {loadError && (
        <div className="flex flex-wrap items-center gap-3">
          <FormError>{loadError}</FormError>
          <Button size="sm" onClick={load}>
            Retry
          </Button>
        </div>
      )}

      <GrantList
        title="Users"
        empty="No users have been given access."
        rows={grants?.map(g => ({ id: g.id, who: g.username, permission: g.permission })) ?? null}
        icon={<User size={16} aria-hidden />}
        onRevoke={(id, who) => setRevoking({ kind: 'user', id, who })}
      />
      <GrantList
        title="Teams"
        empty="No teams have been given access."
        rows={teamGrants?.map(g => ({ id: g.id, who: g.team_name, permission: g.permission })) ?? null}
        icon={<Users size={16} aria-hidden />}
        onRevoke={(id, who) => setRevoking({ kind: 'team', id, who })}
      />

      <form onSubmit={grant} noValidate className="flex flex-col gap-3 border-t border-vx-hairline pt-4">
        <h3 className="vx-label">Give access</h3>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="To" className="flex w-28 flex-col gap-2">
            <select
              className="vx-input"
              value={target}
              onChange={e => {
                setTarget(e.target.value as 'user' | 'team');
                setUserId('');
                setTeamId('');
                setGrantError('');
              }}
            >
              <option value="user">User</option>
              <option value="team">Team</option>
            </select>
          </Field>
          {target === 'user' ? (
            <Field label="User" className="flex min-w-[160px] flex-1 flex-col gap-2">
              <select className="vx-input" value={userId} onChange={e => setUserId(e.target.value)}>
                <option value="">Select a user</option>
                {users
                  .filter(u => u.role !== 'admin')
                  .map(u => (
                    <option key={u.id} value={u.id}>
                      {u.display_name || u.username}
                    </option>
                  ))}
              </select>
            </Field>
          ) : (
            <Field label="Team" className="flex min-w-[160px] flex-1 flex-col gap-2">
              <select className="vx-input" value={teamId} onChange={e => setTeamId(e.target.value)}>
                <option value="">Select a team</option>
                {teams.map(t => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </Field>
          )}
          <Field label="Permission" className="flex w-32 flex-col gap-2">
            <select className="vx-input" value={permission} onChange={e => setPermission(e.target.value)}>
              <option value="view">view</option>
              <option value="control">control</option>
              <option value="manage">manage</option>
            </select>
          </Field>
          <Button type="submit" size="lg" disabled={granting}>
            {granting ? 'Granting…' : 'Grant'}
          </Button>
        </div>
        <p className="text-[13px] text-vx-fg-muted">
          View is watch only. Control also taps and types. Manage also changes the phone&rsquo;s settings.
        </p>
        <FormError>{grantError}</FormError>
      </form>

      <ConfirmDialog
        open={revoking !== null}
        onClose={() => setRevoking(null)}
        title={revoking ? `Revoke ${revoking.who}’s access to ${group.name}?` : ''}
        consequence={`They can’t open phones in ${group.name} through this grant any more. Access they have another way is not affected.`}

        confirmLabel="Revoke"
        formatError={err => errorMessage(err, 'revoke access')}
        onConfirm={async () => {
          if (!revoking) return;
          if (revoking.kind === 'user') await revokeAccess(group.id, revoking.id);
          else await revokeGroupTeamAccess(group.id, revoking.id);
          toast.success(`Revoked ${revoking.who}’s access to ${group.name}.`);
          await load();
        }}
      />
    </div>
  );
}

function GrantList({
  title,
  empty,
  rows,
  icon,
  onRevoke,
}: {
  title: string;
  empty: string;
  rows: { id: string; who: string; permission: string }[] | null;
  icon: React.ReactNode;
  onRevoke: (id: string, who: string) => void;
}) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="vx-label">{title}</h3>
      {rows === null ? (
        <p className="text-sm text-vx-fg-muted">Loading…</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-vx-fg-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-vx-hairline border border-vx-hairline">
          {rows.map(r => (
            <li key={r.id} className="flex min-h-touch items-center gap-3 px-3 py-2">
              <span className="text-vx-fg-label">{icon}</span>
              <span className="min-w-0 flex-1 truncate text-sm text-vx-fg">{r.who}</span>
              <PermissionChip permission={r.permission} />
              <Button
                variant="quiet"
                size="sm"
                icon={<X size={14} />}
                aria-label={`Revoke ${r.who}’s access`}
                onClick={() => onRevoke(r.id, r.who)}
              >
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
