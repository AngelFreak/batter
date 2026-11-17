'use client';

import { useState } from 'react';
import { Check } from 'lucide-react';
import { updateGroup, deleteGroup, DeviceGroup } from '@/lib/api';
import { errorMessage } from '@/lib/api-error';
import { Button, ConfirmDialog, Field, FormError, useToast } from '@/components/ui';

// Stored as hex on the group, so the choices stay as they were.
const COLOR_OPTIONS: { value: string; name: string }[] = [
  { value: '#6366f1', name: 'Indigo' },
  { value: '#3b82f6', name: 'Blue' },
  { value: '#10b981', name: 'Green' },
  { value: '#f59e0b', name: 'Amber' },
  { value: '#ef4444', name: 'Red' },
  { value: '#8b5cf6', name: 'Violet' },
  { value: '#ec4899', name: 'Pink' },
  { value: '#06b6d4', name: 'Cyan' },
];

/** Admin only: rename, describe, colour and delete a group. */
export function GroupSettingsTab({
  group,
  onSaved,
  onDeleted,
}: {
  group: DeviceGroup;
  onSaved: () => void;
  onDeleted: () => void;
}) {
  const toast = useToast();
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description || '');
  const [color, setColor] = useState(group.color);
  const [nameError, setNameError] = useState('');
  const [formError, setFormError] = useState('');
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError('');
    if (!name.trim()) {
      setNameError('Enter a name for the group.');
      return;
    }
    setNameError('');
    setSaving(true);
    try {
      await updateGroup(group.id, { name: name.trim(), description, color });
      toast.success(`Saved ${name.trim()}.`);
      onSaved();
    } catch (err) {
      setFormError(errorMessage(err, 'save the group'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <form onSubmit={save} noValidate className="flex max-w-lg flex-col gap-4">
        <Field label="Name" error={nameError}>
          <input type="text" className="vx-input" value={name} onChange={e => setName(e.target.value)} autoComplete="off" />
        </Field>
        <Field label="Description">
          <input
            type="text"
            className="vx-input"
            value={description}
            onChange={e => setDescription(e.target.value)}
            autoComplete="off"
          />
        </Field>
        <div className="flex flex-col gap-2" role="radiogroup" aria-label="Colour">
          <span className="vx-label" aria-hidden>
            Colour
          </span>
          <div className="flex flex-wrap gap-2">
            {COLOR_OPTIONS.map(c => (
              <button
                key={c.value}
                type="button"
                role="radio"
                aria-checked={color === c.value}
                aria-label={c.name}
                title={c.name}
                onClick={() => setColor(c.value)}
                style={{ backgroundColor: c.value }}
                className={`flex h-9 w-9 items-center justify-center rounded-sm border-2 text-vx-canvas ${
                  color === c.value ? 'border-vx-fg' : 'border-transparent'
                }`}
              >
                {color === c.value && <Check size={16} strokeWidth={3} aria-hidden />}
              </button>
            ))}
          </div>
        </div>
        <FormError>{formError}</FormError>
        <div>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? 'Saving…' : 'Save changes'}
          </Button>
        </div>
      </form>

      <div className="flex flex-col items-start gap-2 border-t border-vx-hairline pt-4">
        <h3 className="vx-label">Delete group</h3>
        <p className="max-w-prose text-sm text-vx-fg-muted">
          Removes the group. The phones stay registered; access given through this group is removed.
        </p>
        <Button variant="danger" onClick={() => setConfirmDelete(true)}>
          Delete group
        </Button>
      </div>

      <ConfirmDialog
        open={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        title={`Delete ${group.name}?`}
        consequence={`${group.member_count} phone${group.member_count !== 1 ? 's' : ''} leave${group.member_count === 1 ? 's' : ''} the group and stay registered. Anyone who had access through this group loses it.`}
        confirmLabel="Delete group"
        formatError={err => errorMessage(err, 'delete the group')}
        onConfirm={async () => {
          await deleteGroup(group.id);
          toast.success(`Deleted ${group.name}.`);
          onDeleted();
        }}
      />
    </div>
  );
}
