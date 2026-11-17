'use client';

import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { X } from 'lucide-react';
import { Button, type ButtonVariant } from './button';
import { cx } from './cx';

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  /** One line under the title. */
  description?: ReactNode;
  children?: ReactNode;
  /** Buttons, right-aligned at the bottom. */
  footer?: ReactNode;
  size?: 'sm' | 'md' | 'lg';
  /** While true, Esc and the close button do nothing (e.g. during a request). */
  busy?: boolean;
}

const WIDTH = { sm: 'max-w-md', md: 'max-w-lg', lg: 'max-w-2xl' };

/**
 * A modal dialog on the native <dialog>: the page behind is inert, focus is
 * trapped inside, Esc closes it, and focus returns to where it was.
 */
export function Dialog({ open, onClose, title, description, children, footer, size = 'md', busy }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const returnTo = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const descId = useId();

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      returnTo.current = document.activeElement as HTMLElement | null;
      d.showModal();
    } else if (!open && d.open) {
      d.close();
    }
  }, [open]);

  // Return focus when it closes, whichever way it closed.
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    const onClosed = () => {
      returnTo.current?.focus?.();
      returnTo.current = null;
    };
    d.addEventListener('close', onClosed);
    return () => d.removeEventListener('close', onClosed);
  }, []);

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      aria-describedby={description ? descId : undefined}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
      onClick={(e) => {
        // A click on the backdrop lands on the dialog element itself.
        if (e.target === ref.current && !busy) onClose();
      }}
      className={cx(
        'w-[calc(100vw-32px)] max-h-[calc(100dvh-32px)] p-0 m-auto',
        'bg-vx-surface text-vx-fg border border-vx-control rounded-md',
        WIDTH[size],
      )}
    >
      {open && (
        <div className="flex max-h-[calc(100dvh-34px)] flex-col">
          <header className="flex items-start gap-4 border-b border-vx-hairline px-5 py-4">
            <div className="min-w-0 flex-1">
              <h2 id={titleId} className="text-lg leading-tight">
                {title}
              </h2>
              {description && (
                <p id={descId} className="mt-1.5 text-sm text-vx-fg-muted">
                  {description}
                </p>
              )}
            </div>
            <Button variant="quiet" size="sm" iconOnly aria-label="Close" icon={<X size={16} />} onClick={onClose} disabled={busy} />
          </header>
          {children && <div className="overflow-y-auto px-5 py-4">{children}</div>}
          {footer && (
            <footer className="flex flex-wrap justify-end gap-2 border-t border-vx-hairline px-5 py-3">{footer}</footer>
          )}
        </div>
      )}
    </dialog>
  );
}

export interface ConfirmDialogProps {
  open: boolean;
  onClose: () => void;
  /** Runs the action. Throw to keep the dialog open and show the error. */
  onConfirm: () => Promise<void> | void;
  title: string;
  /** What happens, in a sentence or two. Names the target. */
  consequence: ReactNode;
  /** The confirm button repeats the verb: "Stop for everyone", "Remove". */
  confirmLabel: string;
  tone?: 'danger' | 'primary';
  /** Turn a thrown error into the line shown in the dialog. */
  formatError?: (err: unknown) => string;
}

/** Asks before an action that's hard to undo. */
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  consequence,
  confirmLabel,
  tone = 'danger',
  formatError,
}: ConfirmDialogProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (open) setError('');
  }, [open]);

  const confirm = async () => {
    setBusy(true);
    setError('');
    try {
      await onConfirm();
      onClose();
    } catch (err) {
      setError(formatError ? formatError(err) : 'That didn’t work. Try again.');
    } finally {
      setBusy(false);
    }
  };

  const variant: ButtonVariant = tone === 'danger' ? 'danger-solid' : 'primary';
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      size="sm"
      busy={busy}
      footer={
        <>
          <Button variant="quiet" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant={variant} onClick={confirm} disabled={busy} autoFocus={tone !== 'danger'}>
            {busy ? 'Working…' : confirmLabel}
          </Button>
        </>
      }
    >
      <div className="text-[14.5px] text-vx-fg-muted">{consequence}</div>
      {error && (
        <p role="alert" className="mt-3 border-l-2 border-vx-danger bg-vx-danger/10 px-3 py-2 text-sm text-vx-fg">
          {error}
        </p>
      )}
    </Dialog>
  );
}
