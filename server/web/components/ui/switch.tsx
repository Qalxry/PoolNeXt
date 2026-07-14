'use client';

import type { ButtonHTMLAttributes } from 'react';

import { cn } from '@/lib/utils/cn';

type SwitchProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onChange'> & {
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
};

export function Switch({
  checked = false,
  className,
  disabled,
  onCheckedChange,
  type,
  ...props
}: SwitchProps) {
  return (
    <button
      type={type ?? 'button'}
      role="switch"
      aria-checked={checked}
      data-state={checked ? 'checked' : 'unchecked'}
      disabled={disabled}
      onClick={() => {
        if (disabled) {
          return;
        }
        onCheckedChange?.(!checked);
      }}
      className={cn(
        'peer inline-flex h-[18px] w-8 shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors outline-none',
        'focus-visible:ring-2 focus-visible:ring-[var(--brand-primary)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--surface-base)]',
        'disabled:cursor-not-allowed disabled:opacity-50',
        checked
          ? 'bg-[var(--brand-primary)]'
          : 'border-[var(--border-default)] bg-[var(--surface-elevated)]',
        className,
      )}
      {...props}
    >
      <span
        className={cn(
          'pointer-events-none block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform',
          checked ? 'translate-x-[15px]' : 'translate-x-[1px]',
        )}
      />
    </button>
  );
}
