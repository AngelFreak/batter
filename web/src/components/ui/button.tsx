'use client';

import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { cx } from './cx';

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'danger' | 'danger-solid';
export type ButtonSize = 'sm' | 'md' | 'lg';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** primary: the one main action in a view. secondary (default): outlined. */
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** A lucide icon element, drawn before the label. */
  icon?: ReactNode;
  /** Icon-only button. Pass `aria-label`; the label is required for screen readers. */
  iconOnly?: boolean;
}

const VARIANT: Record<ButtonVariant, string> = {
  primary:
    'bg-vx-accent border-vx-accent text-vx-accent-ink font-bold hover:bg-vx-accent-hover hover:border-vx-accent-hover',
  secondary: 'border-vx-control text-vx-fg hover:border-vx-accent hover:text-vx-accent',
  quiet: 'border-transparent text-vx-fg-muted hover:border-vx-control hover:text-vx-fg',
  danger: 'border-vx-danger text-vx-danger hover:bg-vx-danger/10',
  'danger-solid': 'bg-vx-danger border-vx-danger text-vx-canvas font-bold hover:bg-vx-danger/90',
};

const SIZE: Record<ButtonSize, string> = {
  sm: 'h-[30px] px-2.5 text-[11px]',
  md: 'h-9 px-3.5 text-xs',
  lg: 'h-touch px-5 text-[12.5px]',
};

const ICON_ONLY: Record<ButtonSize, string> = {
  sm: 'h-[30px] w-[30px]',
  md: 'h-9 w-9',
  lg: 'h-touch w-touch',
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', icon, iconOnly, className, children, type = 'button', ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      className={cx(
        'inline-flex items-center justify-center gap-2 shrink-0 whitespace-nowrap rounded-sm border bg-transparent',
        'font-mono font-medium uppercase tracking-wide transition-colors',
        'disabled:opacity-50 disabled:cursor-not-allowed disabled:pointer-events-none',
        'aria-pressed:border-vx-accent aria-pressed:text-vx-accent aria-pressed:bg-vx-accent/10',
        VARIANT[variant],
        iconOnly ? ICON_ONLY[size] : SIZE[size],
        className,
      )}
      {...rest}
    >
      {icon}
      {!iconOnly && children}
    </button>
  );
});
