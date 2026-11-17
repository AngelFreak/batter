'use client';

import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { useRouter } from 'next/navigation';
import { Globe, Pencil, Plus, ShieldCheck, Trash2 } from 'lucide-react';
import { getToken, isAdmin } from '@/lib/auth';
import {
  listVPNProfiles, createVPNProfile, updateVPNProfile, deleteVPNProfile, checkVPNProfileExitIP,
  type VPNProfile,
} from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { Banner, Button, ConfirmDialog, Dialog, EmptyState, Field, FormError, PageHeader, StatusGlyph, cx, useToast } from '@/components/ui';
import { PageBody } from '@/components/admin/page-body';

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MiB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GiB`;
}

function formatHandshake(iso?: string): string {
  if (!iso) return 'never';
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.round(secs / 60)}m ago`;
  return new Date(iso).toLocaleString();
}

function phones(n: number): string {
  return `${n} ${n === 1 ? 'phone' : 'phones'}`;
}

const configPlaceholder =
  '[Interface]\nPrivateKey = ...\nAddress = 10.64.0.2/32\nDNS = 10.64.0.1\n\n[Peer]\nPublicKey = ...\nEndpoint = vpn.example.net:51820\nAllowedIPs = 0.0.0.0/0';

type ExitResult = { state: 'checking' } | { state: 'ok'; ip: string } | { state: 'blocked' } | { state: 'failed' };

function Switch({ on, disabled, label, onChange }: { on: boolean; disabled: boolean; label: string; onChange: () => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={onChange}
      disabled={disabled}
      className="inline-flex min-h-touch items-center gap-3 rounded-sm font-mono text-[11px] uppercase tracking-wide text-vx-fg-muted disabled:opacity-50"
    >
      <span
        className={cx(
          'relative inline-flex h-5 w-9 shrink-0 items-center rounded-sm border transition-colors',
          on ? 'border-vx-accent bg-vx-accent/20' : 'border-vx-control bg-vx-canvas',
        )}
      >
        <span
          className={cx(
            'inline-block h-3 w-3 rounded-sm transition-transform',
            on ? 'translate-x-[19px] bg-vx-accent' : 'translate-x-[3px] bg-vx-fg-muted',
          )}
        />
      </span>
      {on ? 'On' : 'Off'}
    </button>
  );
}

function Fact({ label, children, wide }: { label: string; children: React.ReactNode; wide?: boolean }) {
  return (
    <div className={cx('bg-vx-surface px-4 py-3', wide && 'sm:col-span-2')}>
      <dt className="vx-label">{label}</dt>
      <dd className="mt-1 break-words text-sm text-vx-fg">{children}</dd>
    </div>
  );
}

// ProfileDialog creates a profile, or edits one (a blank config keeps the saved one).
function ProfileDialog({
  open,
  profile,
  onClose,
  onSubmit,
}: {
  open: boolean;
  profile: VPNProfile | null;
  onClose: () => void;
  onSubmit: (name: string, config: string) => Promise<void>;
}) {
  const formId = useId();
  const nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState('');
  const [config, setConfig] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setName(profile?.name ?? '');
    setConfig('');
    setError('');
    nameRef.current?.focus();
  }, [open, profile]);

  const canSave = name.trim() !== '' && (profile !== null || config.trim() !== '');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !canSave) return;
    setBusy(true);
    setError('');
    try {
      await onSubmit(name, config);
      setConfig('');
      onClose();
    } catch (err) {
      setError(errorMessage(err, profile ? 'save the profile' : 'add the profile'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      busy={busy}
      size="lg"
      title={profile ? `Edit ${profile.name}` : 'New VPN profile'}
      footer={
        <>
          <Button variant="quiet" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" form={formId} variant="primary" disabled={busy || !canSave}>
            {busy ? 'Saving…' : profile ? 'Save' : 'Add profile'}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="space-y-4">
        <Field label="Name" hint="Shown when you choose a profile for a phone, for example Sweden.">
          <input ref={nameRef} className="vx-input" type="text" value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field
          label={profile ? 'WireGuard config (optional)' : 'WireGuard config'}
          hint={
            profile
              ? 'Leave blank to keep the saved config. Paste a new one to replace it. The private key is stored in Batter’s database and is never shown again.'
              : 'A standard wg-quick .conf from your VPN provider. Add a DNS = line: the phones’ DNS queries go to that server, through the tunnel. The private key is stored in Batter’s database and is never shown again.'
          }
        >
          <textarea
            className="vx-input font-mono text-[13px]"
            value={config}
            onChange={(e) => setConfig(e.target.value)}
            rows={10}
            spellCheck={false}
            autoComplete="off"
            autoCapitalize="none"
            placeholder={profile ? 'Paste a new WireGuard config to replace the saved one' : configPlaceholder}
          />
        </Field>
        <FormError>{error}</FormError>
      </form>
    </Dialog>
  );
}

export default function AdminVPNPage() {
  const router = useRouter();
  const toast = useToast();
  const [profiles, setProfiles] = useState<VPNProfile[] | null>(null);
  const [connLost, setConnLost] = useState(false);
  const [dialog, setDialog] = useState<{ profile: VPNProfile | null } | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<VPNProfile | null>(null);
  const [toggling, setToggling] = useState<string | null>(null);
  const [exits, setExits] = useState<Record<string, ExitResult>>({});

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
  }, [router]);

  // A poll: when it fails the last list stays on screen under a banner.
  const refresh = useCallback(async () => {
    try {
      setProfiles(await listVPNProfiles());
      setConnLost(false);
    } catch {
      setConnLost(true);
    }
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 5000);
    return () => clearInterval(t);
  }, [refresh]);

  const toggleEnabled = async (p: VPNProfile) => {
    setToggling(p.id);
    try {
      await updateVPNProfile(p.id, { enabled: !p.enabled });
      toast.success(
        p.enabled
          ? p.devices > 0
            ? `Turned off ${p.name}. Its ${phones(p.devices)} have no internet.`
            : `Turned off ${p.name}.`
          : `Turned on ${p.name}.`,
      );
      await refresh();
    } catch (e) {
      toast.error(errorMessage(e, p.enabled ? 'turn the profile off' : 'turn the profile on'));
    } finally {
      setToggling(null);
    }
  };

  // The server answers 200 with an error field when the check can't get out;
  // that text can carry network detail, so it is not shown.
  const check = async (p: VPNProfile) => {
    setExits((x) => ({ ...x, [p.id]: { state: 'checking' } }));
    try {
      const res = await checkVPNProfileExitIP(p.id);
      if (res.exit_ip) {
        setExits((x) => ({ ...x, [p.id]: { state: 'ok', ip: res.exit_ip as string } }));
        toast.success(`${p.name} exits from ${res.exit_ip}.`);
      } else {
        setExits((x) => ({ ...x, [p.id]: { state: 'blocked' } }));
        toast.error(`${p.name}: no internet for its phones. The tunnel may be down.`);
      }
    } catch (e) {
      setExits((x) => ({ ...x, [p.id]: { state: 'failed' } }));
      toast.error(errorMessage(e, 'check the exit IP'));
    }
  };

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="VPN profiles"
        sub={
          <span>
            The tunnels phones use to reach the internet.
            {profiles && ` ${profiles.length} ${profiles.length === 1 ? 'profile' : 'profiles'}.`}
          </span>
        }
        actions={
          <Button variant="primary" icon={<Plus size={16} />} onClick={() => setDialog({ profile: null })}>
            New profile
          </Button>
        }
      />

      <PageBody>
        <p className="max-w-3xl text-[13.5px] text-vx-fg-muted">
          Phones on the phone network reach the internet only through the profile chosen for them, DNS included. Nothing
          else uses these tunnels: Batter, adb and the host keep their normal route. If a profile is off or its tunnel is
          down, its phones have no internet at all rather than going out directly. Other profiles are unaffected.
        </p>

        {connLost && (
          <Banner tone="danger" actions={<Button size="sm" onClick={refresh}>Retry now</Button>}>
            Connection lost, retrying.
          </Banner>
        )}

        {profiles === null && !connLost && (
          <div role="status" className="border border-vx-hairline bg-vx-surface px-4 py-10 text-center text-sm text-vx-fg-muted">
            Loading profiles…
          </div>
        )}

        {profiles?.length === 0 && (
          <EmptyState
            icon={<ShieldCheck />}
            title="No VPN profiles yet"
            action={
              <Button variant="primary" icon={<Plus size={16} />} onClick={() => setDialog({ profile: null })}>
                New profile
              </Button>
            }
          >
            Add a WireGuard profile to give phones internet. Without one, phones on the phone network have none.
          </EmptyState>
        )}

        {profiles?.map((p) => {
          const exit = exits[p.id];
          const tunnel = !p.enabled
            ? { label: 'Off', icon: 'ring' as const, cls: 'text-vx-fg-label' }
            : p.status?.up
              ? { label: 'Tunnel up', icon: 'dot' as const, cls: 'text-vx-accent' }
              : { label: 'Tunnel down', icon: 'triangle' as const, cls: 'text-vx-warn' };
          return (
            <section
              key={p.id}
              aria-labelledby={`vpn-${p.id}`}
              className="border border-vx-hairline bg-vx-surface"
            >
              <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2 border-b border-vx-hairline px-4 py-3">
                <div className="min-w-0">
                  <h2 id={`vpn-${p.id}`} className="truncate text-lg">
                    {p.name}
                  </h2>
                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] uppercase tracking-wide">
                    <span className={cx('inline-flex items-center gap-1.5', tunnel.cls)}>
                      <StatusGlyph icon={tunnel.icon} className={tunnel.icon === 'dot' ? 'vx-pulse' : undefined} />
                      {tunnel.label}
                    </span>
                    <span className="text-vx-fg-label">{phones(p.devices)}</span>
                    {!p.enabled && <span className="text-vx-fg-label normal-case">Its phones have no internet</span>}
                  </div>
                </div>
                <Switch
                  on={p.enabled}
                  disabled={toggling === p.id}
                  label={`${p.name} enabled`}
                  onChange={() => toggleEnabled(p)}
                />
              </div>

              {p.apply_error && (
                <div className="border-b border-vx-hairline px-4 py-3">
                  {/* apply_error is a raw command error; the server logs it. */}
                  <Banner tone="warn">
                    Saved, but not applied: the kill switch or tunnel didn&rsquo;t come up, so its phones have no internet. The reason is in the server log.
                  </Banner>
                </div>
              )}

              <dl className="grid gap-px border-b border-vx-hairline bg-vx-hairline sm:grid-cols-2">
                <Fact label="Address">
                  <span className="font-mono text-[13px]">{p.config.addresses.join(', ')}</span>
                </Fact>
                <Fact label="DNS for phones">
                  <span className="font-mono text-[13px]">{p.config.dns?.join(', ') || 'default (8.8.8.8)'}</span>
                </Fact>
                <Fact label="This server’s public key" wide>
                  <span className="break-all font-mono text-[13px]">{p.config.public_key}</span>
                </Fact>
                {p.config.peers.flatMap((peer) => {
                  const live = p.status?.peers.find((s) => s.public_key === peer.public_key);
                  return [
                    <Fact key={`${peer.public_key}-ep`} label="Endpoint">
                      <span className="font-mono text-[13px]">{live?.endpoint || peer.endpoint}</span>
                    </Fact>,
                    <Fact key={`${peer.public_key}-hs`} label="Latest handshake">
                      {p.enabled ? formatHandshake(live?.latest_handshake) : '-'}
                    </Fact>,
                    <Fact key={`${peer.public_key}-tr`} label="Received / sent">
                      <span className="font-mono text-[13px]">
                        {live ? `${formatBytes(live.rx_bytes)} / ${formatBytes(live.tx_bytes)}` : '-'}
                      </span>
                    </Fact>,
                    <Fact key={`${peer.public_key}-ip`} label="Allowed IPs">
                      <span className="font-mono text-[13px]">{peer.allowed_ips.join(', ')}</span>
                    </Fact>,
                  ];
                })}
              </dl>

              <div className="flex flex-wrap items-center gap-2 px-4 py-3">
                <Button
                  icon={<Globe size={14} />}
                  className="max-sm:h-touch"
                  disabled={exit?.state === 'checking'}
                  onClick={() => check(p)}
                >
                  {exit?.state === 'checking' ? 'Checking…' : 'Check exit IP'}
                </Button>
                <p role="status" className="min-w-0 text-[13px] text-vx-fg-muted">
                  {exit?.state === 'ok' && (
                    <>
                      Exits from <span className="font-mono text-vx-fg">{exit.ip}</span>
                    </>
                  )}
                  {exit?.state === 'blocked' && (
                    <span className="text-vx-warn">No internet for its phones. The tunnel may be down.</span>
                  )}
                  {exit?.state === 'failed' && <span className="text-vx-danger">The check didn’t run.</span>}
                </p>
                <span className="flex-1" />
                <Button
                  icon={<Pencil size={14} />}
                  className="max-sm:h-touch"
                  aria-label={`Edit ${p.name}`}
                  onClick={() => setDialog({ profile: p })}
                >
                  Edit
                </Button>
                <Button
                  variant="danger"
                  icon={<Trash2 size={14} />}
                  className="max-sm:h-touch"
                  aria-label={`Delete ${p.name}`}
                  onClick={() => setDeleteTarget(p)}
                >
                  Delete
                </Button>
              </div>
            </section>
          );
        })}
      </PageBody>

      <ProfileDialog
        open={!!dialog}
        profile={dialog?.profile ?? null}
        onClose={() => setDialog(null)}
        onSubmit={async (name, config) => {
          const editing = dialog?.profile;
          if (editing) {
            const changes = config.trim() ? { name, config } : { name };
            await updateVPNProfile(editing.id, changes);
            toast.success(`Saved ${name}.`);
          } else {
            await createVPNProfile(name, config);
            toast.success(`Added ${name}.`);
          }
          await refresh();
        }}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title={deleteTarget ? `Delete ${deleteTarget.name}?` : ''}
        confirmLabel="Delete"
        consequence={
          deleteTarget &&
          (deleteTarget.devices > 0 ? (
            <>
              {phones(deleteTarget.devices)} use this profile and will lose internet: the kill switch blocks their
              traffic until you choose another profile for them. This can&apos;t be undone.
            </>
          ) : (
            <>No phones use this profile. This can&apos;t be undone.</>
          ))
        }
        formatError={(e) => errorMessage(e, 'delete the profile')}
        onConfirm={async () => {
          if (!deleteTarget) return;
          const res = await deleteVPNProfile(deleteTarget.id);
          const off = res.devices_turned_off?.length ?? 0;
          toast.success(
            off > 0
              ? `Deleted ${deleteTarget.name}. ${phones(off)} now have no internet.`
              : `Deleted ${deleteTarget.name}.`,
          );
          await refresh();
        }}
      />
    </>
  );
}
