'use client';

import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from 'react';

export interface FieldProps {
  label: string;
  /** A rule or explanation shown under the input. */
  hint?: ReactNode;
  /** An error for this field; replaces nothing, shown under the hint. */
  error?: string;
  /** One input, select or textarea. Its id and aria wiring are filled in. */
  children: ReactElement;
  className?: string;
}

/** A label linked to its input, with a hint and an error slot. */
export function Field({ label, hint, error, children, className }: FieldProps) {
  const auto = useId();
  const id = (isValidElement(children) && (children.props as { id?: string }).id) || auto;
  const hintId = hint ? `${id}-hint` : undefined;
  const errId = error ? `${id}-err` : undefined;
  const describedBy = [hintId, errId].filter(Boolean).join(' ') || undefined;
  const input = cloneElement(children as ReactElement<Record<string, unknown>>, {
    id,
    'aria-describedby': describedBy,
    'aria-invalid': error ? true : undefined,
  });
  return (
    <div className={className ?? 'flex flex-col gap-2'}>
      <label htmlFor={id} className="vx-label">
        {label}
      </label>
      {input}
      {hint && (
        <p id={hintId} className="text-[13px] text-vx-fg-muted">
          {hint}
        </p>
      )}
      {error && (
        <p id={errId} className="text-[13px] text-vx-danger">
          {error}
        </p>
      )}
    </div>
  );
}

/** A form-level error, e.g. what the server said. */
export function FormError({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return (
    <p role="alert" className="border-l-2 border-vx-danger bg-vx-danger/10 px-3 py-2 text-sm text-vx-fg">
      {children}
    </p>
  );
}
