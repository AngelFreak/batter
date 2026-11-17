'use client';

import { ClipboardCopy, ClipboardPaste, Send, X } from 'lucide-react';
import { Button, Field } from '@/components/ui';

/** Type here and Enter sends the text to the phone's focused field. */
export function TextBar({
  value,
  onChange,
  onSend,
  onClose,
  disabled,
  reason,
}: {
  value: string;
  onChange: (v: string) => void;
  onSend: () => void;
  onClose: () => void;
  disabled: boolean;
  reason?: string;
}) {
  return (
    <form
      className="flex items-center gap-2 border-t border-vx-hairline bg-vx-surface px-3 py-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (!disabled) onSend();
      }}
    >
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        // These keys belong to the field, not the phone screen.
        onKeyDown={(e) => e.stopPropagation()}
        placeholder="Type, Enter to send"
        aria-label="Text to send to the phone"
        className="vx-input min-w-0 flex-1"
        autoComplete="off"
        autoFocus
      />
      <Button
        type="submit"
        size="lg"
        icon={<Send size={16} />}
        disabled={disabled || !value.trim()}
        title={disabled ? reason : undefined}
      >
        Send
      </Button>
      <Button variant="quiet" size="lg" iconOnly aria-label="Close keyboard bar" icon={<X size={16} />} onClick={onClose} />
    </form>
  );
}

/** Text to put on the phone's clipboard, or the phone's clipboard fetched here. */
export function ClipboardPanel({
  value,
  onChange,
  onPasteToPhone,
  onCopyFromPhone,
  onClose,
  disabled,
  reason,
}: {
  value: string;
  onChange: (v: string) => void;
  onPasteToPhone: () => void;
  onCopyFromPhone: () => void;
  onClose: () => void;
  disabled: boolean;
  reason?: string;
}) {
  return (
    <section aria-label="Clipboard" className="border-t border-vx-hairline bg-vx-surface px-3 py-3">
      <div className="mb-2 flex items-center justify-between">
        <h2 className="vx-label !font-normal">Clipboard</h2>
        <Button variant="quiet" size="sm" iconOnly aria-label="Close clipboard panel" icon={<X size={16} />} onClick={onClose} />
      </div>
      <Field
        label="Clipboard text"
        hint="Paste to phone sends this text to the phone's focused field. If it is empty, your own clipboard is sent. Copy from phone brings the phone's clipboard here."
      >
        <textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => e.stopPropagation()}
          rows={2}
          className="vx-input font-mono !text-[13px]"
        />
      </Field>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button icon={<ClipboardPaste size={16} />} onClick={onPasteToPhone} disabled={disabled} title={disabled ? reason : undefined}>
          Paste to phone
        </Button>
        <Button icon={<ClipboardCopy size={16} />} onClick={onCopyFromPhone} disabled={disabled} title={disabled ? reason : undefined}>
          Copy from phone
        </Button>
      </div>
    </section>
  );
}
