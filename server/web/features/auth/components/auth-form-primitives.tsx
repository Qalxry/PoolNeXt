import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
} from 'react';

import { cn } from '@/lib/utils/cn';

export function AuthFormField({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="block space-y-1">
      <span className="text-[11px] font-medium text-[var(--foreground-secondary)]">
        {label}
      </span>
      {children}
      {hint ? (
        <span className="block text-[11px] text-[var(--foreground-muted)]">
          {hint}
        </span>
      ) : null}
    </label>
  );
}

export function AuthInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={cn(
        'h-9 w-full rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] px-3 py-1.5 text-[13px] text-[var(--foreground-primary)] transition outline-none placeholder:text-[var(--foreground-muted)] focus:border-[var(--brand-primary)]',
        props.className,
      )}
    />
  );
}

export function AuthButton({
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...props}
      className={cn(
        'inline-flex h-9 w-full items-center justify-center rounded-[var(--radius-control)] bg-[var(--brand-primary)] px-3 text-xs font-medium text-[var(--foreground-inverse)] transition hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
    />
  );
}

export function SecondaryButton({
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...props}
      className={cn(
        'inline-flex h-9 items-center justify-center rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] px-3 text-xs font-medium text-[var(--foreground-primary)] transition hover:bg-[var(--control-background-hover)] disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
    />
  );
}
