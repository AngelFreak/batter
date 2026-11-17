'use client';

import { useEffect, useRef, useState } from 'react';
import { Trash2 } from 'lucide-react';
import { updateDevice, deleteDevice, setTether, listVPNProfileNames, DeviceInfo, VPNProfileName } from '@/lib/api';
import { isOperator } from '@/lib/auth';
import { networkLabel } from '@/lib/device-network';
import { displayName } from '@/lib/dashboard-filter';
import { errorMessage } from '@/lib/api-error';
import { Button, ConfirmDialog, Dialog, Field, FormError, useToast } from '@/components/ui';
import { EthernetPanel } from './ethernet-panel';

interface DeviceEditModalProps {
  /** The phone being edited. Null keeps the dialog closed. */
  device: DeviceInfo | null;
  onClose: () => void;
  onSaved: () => void;
  onDeleted: () => void;
}

const FORM_ID = 'edit-device-form';

function Fact({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="vx-label">{label}</dt>
      <dd className={`mt-1 break-words text-sm text-vx-fg ${mono ? 'font-mono text-[13px]' : ''}`}>{value}</dd>
    </div>
  );
}

/**
 * Mount it fresh for each edit (the dashboard keys it), so the form starts from
 * the phone's current values. It stays mounted while closed, so Dialog can hand
 * focus back to where it was.
 */
export function DeviceEditModal({ device, onClose, onSaved, onDeleted }: DeviceEditModalProps) {
  const toast = useToast();
  // The last phone shown, so the dialog's content survives while it closes.
  const lastDevice = useRef<DeviceInfo | null>(device);
  if (device) lastDevice.current = device;
  const shown = lastDevice.current;
  const [nickname, setNickname] = useState(device?.nickname || '');
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [profileId, setProfileId] = useState(device?.vpn_profile_id ?? '');
  const [profiles, setProfiles] = useState<VPNProfileName[] | null>(null);
  const [tetherBusy, setTetherBusy] = useState(false);
  const [tetherNote, setTetherNote] = useState('');
  const [tetherError, setTetherError] = useState('');
  // The VPN being turned off, waiting for confirmation.
  const [confirmVpnOff, setConfirmVpnOff] = useState(false);

  const open = device !== null;

  useEffect(() => {
    if (!open) return;
    listVPNProfileNames()
      .then(setProfiles)
      .catch(() => setProfiles([]));
  }, [open]);

  const name = shown ? displayName(shown) : '';

  // Throws when the request fails; the caller decides where the error shows.
  const applyTether = async (id: string) => {
    if (!shown) return;
    const res = await setTether(shown.serial, id || null);
    setProfileId(res.vpn_profile_id ?? '');
    if (res.apply_error) {
      setTetherNote('Saved, but not applied to the phone yet. Check that it is connected.');
    } else {
      setTetherNote('');
    }
    toast.success(id ? 'Internet through the VPN set.' : 'VPN turned off.');
  };

  const handleTetherChange = async (id: string) => {
    setTetherError('');
    setTetherNote('');
    // Turning the VPN off cuts the phone's internet, so it asks first.
    if (!id && profileId) {
      setConfirmVpnOff(true);
      return;
    }
    setTetherBusy(true);
    try {
      await applyTether(id);
    } catch (err) {
      setTetherError(errorMessage(err, "change the phone's internet"));
    } finally {
      setTetherBusy(false);
    }
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!shown) return;
    setSaving(true);
    setFormError('');
    try {
      await updateDevice(shown.serial, { nickname });
      toast.success('Saved.');
      onSaved();
    } catch (err) {
      setFormError(errorMessage(err, 'save the changes'));
    } finally {
      setSaving(false);
    }
  };

  const noProfiles = profiles !== null && profiles.length === 0;

  return (
    <>
      <Dialog
        open={open}
        onClose={onClose}
        title="Edit phone"
        description={name}
        busy={saving}
        footer={
          <>
            <Button variant="quiet" onClick={onClose} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" form={FORM_ID} variant="primary" disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </>
        }
      >
        {shown && (
        <div className="flex flex-col gap-6">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-4">
            <Fact label="Serial" value={shown?.serial ?? ''} mono />
            <Fact label="Model" value={shown.model || 'Unknown'} />
            <Fact label="Product" value={shown.product || 'Unknown'} />
            <Fact label="Android version" value={shown.android_version || 'Unknown'} />
          </dl>

          <form id={FORM_ID} onSubmit={handleSave} className="flex flex-col gap-5">
            <Field label="Nickname" hint="Shown instead of the model name.">
              <input
                type="text"
                name="nickname"
                value={nickname}
                onChange={(e) => setNickname(e.target.value)}
                placeholder="e.g. Test phone 1"
                className="vx-input"
                autoComplete="off"
              />
            </Field>
            <FormError>{formError}</FormError>
          </form>

          <Field
            label="Internet"
            error={tetherError}
            hint={
              noProfiles
                ? 'No VPN profiles yet. Ask an admin to add one to give the phone internet.'
                : 'On the phone network (ethernet adapter), the phone reaches the internet only through the chosen VPN, never directly. None means no internet. On USB a phone has no internet: USB is for setup and control.'
            }
          >
            <select
              value={profileId}
              onChange={(e) => handleTetherChange(e.target.value)}
              disabled={tetherBusy || profiles === null || (noProfiles && !profileId)}
              className="vx-input disabled:opacity-50"
            >
              <option value="">None</option>
              {profiles?.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </Field>
          {tetherNote && (
            <p role="status" className="-mt-4 border-l-2 border-vx-warn bg-vx-warn/10 px-3 py-2 text-sm text-vx-fg">
              {tetherNote}
            </p>
          )}

          <div className="flex flex-col gap-2">
            <span className="vx-label">Connection</span>
            <p className="text-sm text-vx-fg">{networkLabel(shown)}</p>
            {isOperator() && (shown.connection !== 'lan' || shown.needs_reprovision) && (
              <EthernetPanel serial={shown.serial} device={shown} />
            )}
          </div>

          <div className="flex flex-col items-start gap-2 border-t border-vx-hairline pt-4">
            <span className="vx-label">Remove</span>
            <p className="text-sm text-vx-fg-muted">
              Takes this phone off Batter. The phone itself is not touched, and it can be registered again.
            </p>
            <Button variant="danger" size="sm" icon={<Trash2 size={14} />} onClick={() => setConfirmDelete(true)}>
              Remove phone
            </Button>
          </div>
        </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        title={`Remove ${name} from Batter?`}
        consequence="The phone itself is not touched. It can be registered again."
        confirmLabel="Remove"
        formatError={(err) => errorMessage(err, 'remove the phone')}
        onConfirm={async () => {
          if (!shown) return;
          await deleteDevice(shown.serial);
          toast.success(`Removed ${name}.`);
          onDeleted();
        }}
      />

      <ConfirmDialog
        open={confirmVpnOff}
        onClose={() => setConfirmVpnOff(false)}
        title={`Turn off the VPN for ${name}?`}
        consequence="The phone will have no internet until a VPN profile is set again."
        confirmLabel="Turn off"
        formatError={(err) => errorMessage(err, "change the phone's internet")}
        onConfirm={async () => {
          await applyTether('');
        }}
      />
    </>
  );
}
