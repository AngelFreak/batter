'use client';

import { useEffect, useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import { ChevronDown, FolderTree, Play, Plus, Square } from 'lucide-react';
import {
  listGroups, listDevices, batchStartGroup, batchStopGroup, getGroupDevices,
  DeviceGroup, DeviceInfo,
} from '@/lib/api';
import { getToken } from '@/lib/auth';
import { errorMessage } from '@/lib/api-error';
import { useUser } from '@/lib/use-user';
import { Banner, Button, ConfirmDialog, EmptyState, PageHeader, cx, useToast } from '@/components/ui';
import { CreateGroupDialog } from '@/components/groups/create-group-dialog';
import { GroupPanel } from '@/components/groups/group-panel';

/** What the batch endpoints report. `skipped` counts phones the caller may not use. */
interface BatchOutcome {
  started?: number;
  stopped?: number;
  failed?: number;
  skipped?: number;
}

const phones = (n: number) => `${n} phone${n !== 1 ? 's' : ''}`;

export default function GroupsPage() {
  const router = useRouter();
  const toast = useToast();
  const { isAdmin } = useUser();
  const [groups, setGroups] = useState<DeviceGroup[]>([]);
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [expandedGroupId, setExpandedGroupId] = useState<string | null>(null);
  const [batchLoading, setBatchLoading] = useState<Record<string, boolean>>({});
  const [stopTarget, setStopTarget] = useState<{ group: DeviceGroup; n: number } | null>(null);

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); }
  }, [router]);

  const refresh = useCallback(async () => {
    try {
      const [g, d] = await Promise.all([listGroups(), listDevices()]);
      setGroups(g);
      setDevices(d);
      setLoadFailed(false);
    } catch {
      // Keep what is on screen and offer a retry.
      setLoadFailed(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  const setBusy = (id: string, busy: boolean) => setBatchLoading(prev => ({ ...prev, [id]: busy }));

  const handleBatchStart = async (group: DeviceGroup) => {
    setBusy(group.id, true);
    try {
      const result: BatchOutcome = await batchStartGroup(group.id, 360, 5);
      const started = result.started ?? 0;
      const failed = result.failed ?? 0;
      const skipped = result.skipped ?? 0;
      const extra = skipped > 0 ? ` ${phones(skipped)} skipped: no access.` : '';
      if (failed > 0) {
        toast.error(`Started ${started} of ${started + failed} in ${group.name}; ${failed} failed.${extra}`);
      } else if (started === 0) {
        toast.info(`No phones to start in ${group.name}.${extra}`);
      } else {
        toast.success(`Started ${phones(started)} in ${group.name}.${extra}`);
      }
      refresh();
    } catch (err) {
      toast.error(errorMessage(err, `start the sessions in ${group.name}`));
    } finally {
      setBusy(group.id, false);
    }
  };

  // Stop asks first, naming how many sessions it ends. That count comes from
  // the group's phones that have a session right now.
  const askStop = async (group: DeviceGroup) => {
    setBusy(group.id, true);
    try {
      const serials = await getGroupDevices(group.id);
      const n = devices.filter(d => d.has_session && serials.includes(d.serial)).length;
      if (n === 0) {
        toast.info(`No sessions are running in ${group.name}.`);
      } else {
        setStopTarget({ group, n });
      }
    } catch (err) {
      toast.error(errorMessage(err, `check the sessions in ${group.name}`));
    } finally {
      setBusy(group.id, false);
    }
  };

  const confirmStop = async () => {
    if (!stopTarget) return;
    const { group } = stopTarget;
    setBusy(group.id, true);
    try {
      const result: BatchOutcome = await batchStopGroup(group.id);
      const stopped = result.stopped ?? 0;
      const failed = result.failed ?? 0;
      const skipped = result.skipped ?? 0;
      const extra = skipped > 0 ? ` ${phones(skipped)} skipped: no access.` : '';
      if (failed > 0) {
        toast.error(`Stopped ${stopped} of ${stopped + failed} in ${group.name}; ${failed} failed.${extra}`);
      } else {
        toast.success(`Stopped ${stopped} session${stopped !== 1 ? 's' : ''} in ${group.name}.${extra}`);
      }
      refresh();
    } finally {
      setBusy(group.id, false);
    }
  };

  const toggleExpand = (groupId: string) => {
    setExpandedGroupId(prev => (prev === groupId ? null : groupId));
  };

  return (
    <>
      <PageHeader
        title="Groups"
        sub={loading ? undefined : `${groups.length} group${groups.length !== 1 ? 's' : ''}`}
        actions={
          isAdmin && (
            <Button variant="primary" icon={<Plus size={16} />} onClick={() => setShowCreate(true)}>
              New group
            </Button>
          )
        }
      />

      <div className="flex max-w-5xl flex-col gap-4 p-4 sm:p-6">
        {loadFailed && (
          <Banner
            tone="danger"
            actions={
              <Button size="sm" onClick={refresh}>
                Retry
              </Button>
            }
          >
            Couldn&rsquo;t load groups. {groups.length > 0 ? 'Showing the last list we have.' : ''}
          </Banner>
        )}

        {loading ? (
          <p className="text-sm text-vx-fg-muted">Loading groups…</p>
        ) : groups.length === 0 && !loadFailed ? (
          <EmptyState
            icon={<FolderTree />}
            title="No groups yet"
            action={
              isAdmin ? (
                <Button variant="primary" icon={<Plus size={16} />} onClick={() => setShowCreate(true)}>
                  New group
                </Button>
              ) : undefined
            }
          >
            {isAdmin
              ? 'Create a group to start, stop and share a set of phones together.'
              : 'You can see the groups an admin has shared with you. None yet; ask an admin.'}
          </EmptyState>
        ) : (
          <ul className="flex flex-col gap-px border border-vx-hairline bg-vx-hairline">
            {groups.map(group => {
              const isExpanded = expandedGroupId === group.id;
              const busy = batchLoading[group.id];
              const panelId = `group-panel-${group.id}`;

              return (
                <li key={group.id} className="bg-vx-surface">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-2 pr-3 sm:pr-4">
                    <button
                      type="button"
                      aria-expanded={isExpanded}
                      aria-controls={panelId}
                      onClick={() => toggleExpand(group.id)}
                      className="flex min-h-touch min-w-0 flex-1 basis-60 items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-vx-elevated"
                    >
                      <span
                        aria-hidden
                        className="h-3 w-3 shrink-0 border border-vx-hairline"
                        style={{ backgroundColor: group.color }}
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[15px] font-semibold text-vx-fg">{group.name}</span>
                        {group.description && (
                          <span className="block truncate text-[13px] text-vx-fg-muted">{group.description}</span>
                        )}
                      </span>
                      <span className="whitespace-nowrap font-mono text-[11px] uppercase tracking-wide text-vx-fg-label">
                        {phones(group.member_count)}
                      </span>
                      <ChevronDown
                        size={16}
                        aria-hidden
                        className={cx('shrink-0 text-vx-fg-muted transition-transform', isExpanded && 'rotate-180')}
                      />
                    </button>
                    <div className="flex gap-2 pb-3 pl-4 sm:p-0">
                      <Button
                        size="sm"
                        icon={<Play size={14} />}
                        onClick={() => handleBatchStart(group)}
                        disabled={busy}
                        aria-label={`Start all sessions in ${group.name}`}
                      >
                        {busy ? 'Working…' : 'Start all'}
                      </Button>
                      <Button
                        size="sm"
                        icon={<Square size={12} />}
                        onClick={() => askStop(group)}
                        disabled={busy}
                        aria-label={`Stop all sessions in ${group.name}`}
                      >
                        Stop all
                      </Button>
                    </div>
                  </div>

                  {isExpanded && (
                    <GroupPanel
                      key={group.id}
                      panelId={panelId}
                      group={group}
                      devices={devices}
                      isAdmin={isAdmin}
                      onChanged={refresh}
                      onDeleted={() => {
                        setExpandedGroupId(null);
                        refresh();
                      }}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <CreateGroupDialog open={showCreate} onClose={() => setShowCreate(false)} onCreated={refresh} />

      <ConfirmDialog
        open={stopTarget !== null}
        onClose={() => setStopTarget(null)}
        title={stopTarget ? `Stop ${stopTarget.n} session${stopTarget.n !== 1 ? 's' : ''} in ${stopTarget.group.name}?` : ''}
        consequence="The streams end for everyone watching. The phones keep running."
        confirmLabel={stopTarget ? `Stop ${stopTarget.n} session${stopTarget.n !== 1 ? 's' : ''}` : 'Stop'}
        formatError={err => errorMessage(err, 'stop the sessions')}
        onConfirm={confirmStop}
      />
    </>
  );
}
