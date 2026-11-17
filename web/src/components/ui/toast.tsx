'use client';

import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react';
import { Check, AlertTriangle, X, Info } from 'lucide-react';
import { cx } from './cx';

export type ToastKind = 'success' | 'info' | 'error';

interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ToastApi {
  /** Short confirmation; goes away on its own after 4s. */
  success(message: string): void;
  info(message: string): void;
  /** Stays until dismissed, so it isn't missed. */
  error(message: string): void;
}

const ToastContext = createContext<ToastApi | null>(null);

const MAX = 4;
const TIMEOUT_MS = 4000;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const next = useRef(1);

  const dismiss = useCallback((id: number) => setItems((xs) => xs.filter((t) => t.id !== id)), []);

  const push = useCallback(
    (kind: ToastKind, message: string) => {
      const id = next.current++;
      setItems((xs) => [...xs, { id, kind, message }].slice(-MAX));
      if (kind !== 'error') setTimeout(() => dismiss(id), TIMEOUT_MS);
    },
    [dismiss],
  );

  const api = useMemo<ToastApi>(
    () => ({
      success: (m) => push('success', m),
      info: (m) => push('info', m),
      error: (m) => push('error', m),
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div
        className="pointer-events-none fixed inset-x-4 bottom-4 z-50 flex flex-col items-end gap-2 sm:left-auto sm:right-4 sm:w-[360px]"
        aria-live="polite"
      >
        {items.map((t) => (
          <div
            key={t.id}
            role={t.kind === 'error' ? 'alert' : 'status'}
            className={cx(
              'pointer-events-auto flex w-full items-start gap-3 border border-vx-control border-l-2 bg-vx-elevated px-3.5 py-3 text-sm',
              t.kind === 'success' && 'border-l-vx-accent',
              t.kind === 'info' && 'border-l-vx-fg-muted',
              t.kind === 'error' && 'border-l-vx-danger',
            )}
          >
            {t.kind === 'success' && <Check size={16} className="mt-0.5 shrink-0 text-vx-accent" aria-hidden />}
            {t.kind === 'info' && <Info size={16} className="mt-0.5 shrink-0 text-vx-fg-muted" aria-hidden />}
            {t.kind === 'error' && <AlertTriangle size={16} className="mt-0.5 shrink-0 text-vx-danger" aria-hidden />}
            <p className="min-w-0 flex-1 text-vx-fg">{t.message}</p>
            <button
              type="button"
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss"
              className="-m-1 p-1 text-vx-fg-muted hover:text-vx-fg"
            >
              <X size={14} aria-hidden />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error('useToast must be used inside ToastProvider');
  return ctx;
}
