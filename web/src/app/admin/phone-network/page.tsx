'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Cable } from 'lucide-react';
import { getToken, isAdmin } from '@/lib/auth';
import {
  getPhoneNetwork, listBoxNICs, setPhoneNetworkPort,
  type BoxNIC, type PhoneNetworkStatus,
} from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { describeNIC, pickable, stateSummary } from '@/lib/phone-network';
import { Banner, Button, ConfirmDialog, EmptyState, PageHeader, StatusGlyph, cx, useToast } from '@/components/ui';
import { DataTable } from '@/components/admin/data-table';
import { PageBody } from '@/components/admin/page-body';

const TONE = {
  ok: { icon: 'dot' as const, cls: 'text-vx-accent' },
  off: { icon: 'ring' as const, cls: 'text-vx-fg-muted' },
  bad: { icon: 'triangle' as const, cls: 'text-vx-warn' },
};

export default function PhoneNetworkPage() {
  const router = useRouter();
  const toast = useToast();
  const [status, setStatus] = useState<PhoneNetworkStatus | null>(null);
  const [nics, setNics] = useState<BoxNIC[] | null>(null);
  const [nicError, setNicError] = useState('');
  const [statusError, setStatusError] = useState(false);
  const [choice, setChoice] = useState<string>('');
  const [confirming, setConfirming] = useState(false);
  // First-run: shown right after the admin account is created.
  const [setup, setSetup] = useState(false);

  useEffect(() => {
    if (!getToken()) { router.replace('/login'); return; }
    if (!isAdmin()) { router.replace('/dashboard'); return; }
    setSetup(new URLSearchParams(window.location.search).has('setup'));
  }, [router]);

  // A poll: when it fails the last state stays on screen under a banner.
  const load = useCallback(async () => {
    try {
      const st = await getPhoneNetwork();
      setStatus(st);
      setStatusError(false);
      setChoice((c) => c || st.port?.mac || '');
    } catch {
      setStatusError(true);
    }
    try {
      setNics(await listBoxNICs());
      setNicError('');
    } catch (e) {
      setNics((n) => n ?? []);
      setNicError(errorMessage(e, 'list the network ports'));
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [load]);

  const summary = status ? stateSummary(status) : null;
  const usable = pickable(nics ?? []);
  const refused = (nics ?? []).filter((n) => !n.usable);
  const current = status?.port?.mac ?? '';
  const unchanged = choice === current;
  const chosenNic = nics?.find((n) => n.mac === choice);
  const portName = choice ? chosenNic?.name ?? status?.port?.name ?? choice : '';

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Phone network"
        sub="The box’s network port for the phones’ switch."
        actions={
          setup && (
            <Button
              variant={status?.state === 'active' ? 'primary' : 'secondary'}
              onClick={() => router.push('/dashboard')}
            >
              {status?.state === 'active' ? 'Continue' : 'Skip for now'}
            </Button>
          )
        }
      />

      <PageBody>
        {setup && (
          <Banner tone="info">
            Optional: if the phones will use ethernet adapters, choose the network port their switch is plugged into. You
            can do this later under Admin, Phone network.
          </Banner>
        )}

        <p className="max-w-3xl text-[13.5px] text-vx-fg-muted">
          Phones with an ethernet adapter, on a switch plugged into this port, get their network from Batter and reach the
          internet only through their VPN profile. While Batter uses the port, the box itself has no connection on it.
          Phones on USB are controlled only and get no internet.
        </p>

        {statusError && (
          <Banner tone="danger" actions={<Button size="sm" onClick={load}>Retry now</Button>}>
            Connection lost, retrying.
          </Banner>
        )}

        {status === null && !statusError && (
          <div role="status" className="border border-vx-hairline bg-vx-surface px-4 py-10 text-center text-sm text-vx-fg-muted">
            Loading the phone network…
          </div>
        )}

        {status && summary && (
          <section aria-labelledby="pn-status" className="space-y-3 border border-vx-hairline bg-vx-surface p-4">
            <h2 id="pn-status" className="vx-label">
              Status
            </h2>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <span
                className={cx(
                  'inline-flex items-center gap-2 font-mono text-xs uppercase tracking-wide',
                  TONE[summary.tone].cls,
                )}
              >
                <StatusGlyph icon={TONE[summary.tone].icon} className={status.state === 'active' ? 'vx-pulse' : undefined} />
                {summary.label}
              </span>
              {status.state === 'active' && (
                <span className="font-mono text-xs text-vx-fg-label">
                  Batter {status.address} · phones {status.pool}
                </span>
              )}
            </div>
            {/* error and unavailable can be raw system errors, which the server logs.
                Only "missing" is a fixed sentence written for display. */}
            {status.error && (
              <Banner tone="danger">
                {status.state === 'missing'
                  ? status.error
                  : 'The phone network port couldn\u2019t be set up. The reason is in the server log.'}
              </Banner>
            )}
            {status.unavailable && (
              <Banner tone="danger">Batter can&rsquo;t see the box&rsquo;s network ports. The reason is in the server log.</Banner>
            )}
            {status.warning && <Banner tone="warn">{status.warning}</Banner>}
          </section>
        )}

        {nics !== null && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (!unchanged) setConfirming(true);
            }}
            className="space-y-4 border border-vx-hairline bg-vx-surface p-4"
          >
            <fieldset className="space-y-2">
              <legend className="vx-label mb-2">Port</legend>
              {nicError && (
                <p role="alert" className="border-l-2 border-vx-danger bg-vx-danger/10 px-3 py-2 text-sm text-vx-fg">
                  {nicError}
                </p>
              )}
              <PortOption
                checked={choice === ''}
                onSelect={() => setChoice('')}
                title="Off"
                detail="No phone network. Phones get no internet."
              />
              {usable.map((n) => (
                <PortOption
                  key={n.mac}
                  checked={choice === n.mac}
                  onSelect={() => setChoice(n.mac)}
                  title={n.name}
                  mono
                  detail={`${n.mac} · ${describeNIC(n)}`}
                />
              ))}
              {usable.length === 0 && !nicError && (
                <p className="text-sm text-vx-fg-muted">No network port on the box can be used.</p>
              )}
            </fieldset>

            {refused.length > 0 && (
              <div className="space-y-1 border-t border-vx-hairline pt-3">
                <h3 className="vx-label">Can’t be used</h3>
                <ul className="space-y-1">
                  {refused.map((n) => (
                    <li key={n.mac} className="text-[13px] text-vx-fg-muted">
                      <span className="font-mono text-vx-fg">{n.name}</span>{' '}
                      <span className="font-mono text-xs text-vx-fg-label">({n.mac})</span>: {n.reason}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div className="flex flex-wrap items-center gap-3 border-t border-vx-hairline pt-4">
              <Button type="submit" variant="primary" disabled={unchanged}>
                Apply
              </Button>
              <p className="min-w-0 flex-1 text-[13px] text-vx-fg-muted">
                The box must not manage this port itself (no NetworkManager, netplan or ifupdown configuration): see
                docs/DEPLOY.md. Applying takes effect at once.
              </p>
            </div>
          </form>
        )}

        {status && status.state === 'active' && (
          <section aria-labelledby="pn-clients" className="space-y-3">
            <h2 id="pn-clients" className="vx-label">
              Devices on the network
            </h2>
            {status.clients.length === 0 ? (
              <EmptyState icon={<Cable />} title="No devices yet">
                Plug a phone with an ethernet adapter into the switch. It shows up here once it gets an address.
              </EmptyState>
            ) : (
              <DataTable
                caption="Devices on the phone network"
                rows={status.clients}
                rowKey={(c) => c.mac}
                columns={[
                  { key: 'ip', header: 'Address', primary: true, cell: (c) => <span className="font-mono text-[13px]">{c.ip}</span> },
                  { key: 'mac', header: 'MAC', cell: (c) => <span className="font-mono text-[13px]">{c.mac}</span> },
                  { key: 'name', header: 'Name', cell: (c) => <span className="text-sm">{c.hostname || '—'}</span> },
                  {
                    key: 'phone',
                    header: 'Phone',
                    cell: (c) =>
                      c.serial ? (
                        <span className="font-mono text-[13px]">{c.serial}</span>
                      ) : (
                        <span className="text-sm text-vx-fg-muted">Not a phone (no internet)</span>
                      ),
                  },
                ]}
              />
            )}
          </section>
        )}
      </PageBody>

      <ConfirmDialog
        open={confirming}
        onClose={() => setConfirming(false)}
        tone="primary"
        title={choice ? `Apply port ${portName}?` : 'Turn off the phone network?'}
        confirmLabel={choice ? 'Apply' : 'Turn off'}
        consequence={
          choice ? (
            <>
              Batter takes <span className="font-mono text-vx-fg">{portName}</span> for the phone network and moves it
              out of the box&apos;s own network, so the box has no connection on it. Phones on the network may lose their
              connection while it changes. It takes effect at once.
            </>
          ) : (
            <>Phones on the network lose their connection and get no internet until you choose a port again.</>
          )
        }
        formatError={(e) => errorMessage(e, 'change the phone network')}
        onConfirm={async () => {
          const mac = choice || null;
          setStatus(await setPhoneNetworkPort(mac));
          setChoice(mac ?? '');
          toast.success(mac ? `Phone network applied to ${portName}.` : 'Phone network turned off.');
          await load();
        }}
      />
    </>
  );
}

function PortOption({
  checked,
  onSelect,
  title,
  detail,
  mono,
}: {
  checked: boolean;
  onSelect: () => void;
  title: string;
  detail: string;
  mono?: boolean;
}) {
  return (
    <label
      className={cx(
        'flex min-h-touch cursor-pointer items-start gap-3 border px-3 py-2.5 transition-colors',
        checked ? 'border-vx-accent bg-vx-accent/5' : 'border-vx-hairline hover:border-vx-control',
      )}
    >
      <input type="radio" name="port" checked={checked} onChange={onSelect} className="mt-1 h-4 w-4 accent-vx-accent" />
      <span className="min-w-0">
        <span className={cx('block text-sm text-vx-fg', mono && 'font-mono')}>{title}</span>
        <span className="block break-words font-mono text-xs text-vx-fg-label">{detail}</span>
      </span>
    </label>
  );
}
