'use client';

import { useEffect, useState } from 'react';
import { createGroup } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { Button, Dialog, Field, FormError, useToast } from '@/components/ui';

const FORM_ID = 'create-group-form';

/** Admin only: the API rejects everyone else. */
export function CreateGroupDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const toast = useToast();
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [nameError, setNameError] = useState('');
  const [formError, setFormError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setName('');
      setDescription('');
      setNameError('');
      setFormError('');
    }
  }, [open]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setNameError('Enter a name for the group.');
      return;
    }
    setNameError('');
    setFormError('');
    setBusy(true);
    try {
      await createGroup(trimmed, description.trim() || undefined);
      toast.success(`Group “${trimmed}” created.`);
      onCreated();
      onClose();
    } catch (err) {
      setFormError(errorMessage(err, 'create the group'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      busy={busy}
      size="sm"
      title="New group"
      description="A group collects phones so you can start or stop their sessions together and share access."
      footer={
        <>
          <Button variant="quiet" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" form={FORM_ID} variant="primary" disabled={busy}>
            {busy ? 'Creating…' : 'Create group'}
          </Button>
        </>
      }
    >
      <form id={FORM_ID} onSubmit={submit} noValidate className="flex flex-col gap-4">
        <Field label="Name" error={nameError}>
          <input
            type="text"
            className="vx-input"
            value={name}
            onChange={e => setName(e.target.value)}
            autoComplete="off"
            autoFocus
          />
        </Field>
        <Field label="Description" hint="Optional. Shown under the name.">
          <input
            type="text"
            className="vx-input"
            value={description}
            onChange={e => setDescription(e.target.value)}
            autoComplete="off"
          />
        </Field>
        <FormError>{formError}</FormError>
      </form>
    </Dialog>
  );
}
