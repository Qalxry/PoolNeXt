import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from 'react';
import { CircleHelp } from 'lucide-react';

import { cn } from '@/lib/utils/cn';

interface ResourceFieldProps {
  label: string;
  hint?: string;
  error?: string;
  className?: string;
  tooltip?: string;
  children: ReactNode;
}

function FieldTooltip({ content }: { content: string }) {
  return (
    <span className="group/tooltip relative inline-flex">
      <span
        className="inline-flex h-4 w-4 items-center justify-center text-[var(--foreground-muted)] transition group-hover/tooltip:text-[var(--foreground-primary)]"
        title={content}
      >
        <CircleHelp className="h-3.5 w-3.5" aria-hidden="true" />
      </span>
      <span className="pointer-events-none absolute top-full left-1/2 z-20 mt-1.5 w-64 -translate-x-1/2 rounded-[var(--radius-control)] border border-[var(--border-strong)] bg-[var(--surface-panel)] px-2.5 py-2 text-[11px] leading-4 text-[var(--foreground-secondary)] opacity-0 shadow-[var(--shadow-lg)] transition-opacity delay-500 duration-150 group-hover/tooltip:opacity-100">
        {content}
      </span>
    </span>
  );
}

export function ResourceField({
  label,
  hint,
  error,
  className,
  tooltip,
  children,
}: ResourceFieldProps) {
  return (
    <label className={cn('block space-y-1', className)}>
      <span className="flex items-center gap-1.5 text-[11px] font-medium text-[var(--foreground-secondary)]">
        <span>{label}</span>
        {tooltip ? <FieldTooltip content={tooltip} /> : null}
      </span>
      {children}
      {error ? (
        <span className="block text-[11px] leading-4 text-[var(--status-danger-foreground)]">
          {error}
        </span>
      ) : hint ? (
        <span className="block text-[11px] leading-4 text-[var(--foreground-muted)]">
          {hint}
        </span>
      ) : null}
    </label>
  );
}

export function ResourceInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={cn(
        'h-8 w-full rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] px-2.5 py-1.5 text-[13px] text-[var(--foreground-primary)] transition outline-none placeholder:text-[var(--foreground-muted)] focus:border-[var(--brand-primary)] disabled:cursor-not-allowed disabled:opacity-60',
        props.className,
      )}
    />
  );
}

export function ResourceTextarea(
  props: TextareaHTMLAttributes<HTMLTextAreaElement>,
) {
  return (
    <textarea
      {...props}
      className={cn(
        'min-h-20 w-full rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] px-2.5 py-2 text-[13px] leading-5 text-[var(--foreground-primary)] transition outline-none placeholder:text-[var(--foreground-muted)] focus:border-[var(--brand-primary)] disabled:cursor-not-allowed disabled:opacity-60',
        props.className,
      )}
    />
  );
}

export function ResourceSelect(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(
        'h-8 w-full rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] px-2.5 py-1.5 text-[13px] text-[var(--foreground-primary)] transition outline-none focus:border-[var(--brand-primary)] disabled:cursor-not-allowed disabled:opacity-60',
        props.className,
      )}
    />
  );
}

function baseButtonClassName(className?: string) {
  return cn(
    'inline-flex h-8 min-h-8 items-center justify-center gap-1.5 rounded-[var(--radius-control)] px-2.5 py-1.5 text-xs font-medium whitespace-nowrap transition disabled:cursor-not-allowed disabled:opacity-60',
    className,
  );
}

export function PrimaryButton({
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...props}
      className={baseButtonClassName(
        cn(
          'bg-[var(--brand-primary)] text-[var(--foreground-inverse)] hover:opacity-90',
          className,
        ),
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
      className={baseButtonClassName(
        cn(
          'border border-[var(--border-default)] bg-[var(--control-background)] text-[var(--foreground-primary)] hover:bg-[var(--control-background-hover)]',
          className,
        ),
      )}
    />
  );
}

export function DangerButton({
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...props}
      className={baseButtonClassName(
        cn(
          'border border-[var(--status-danger-border)] bg-[var(--status-danger-soft)] text-[var(--status-danger-foreground)] hover:opacity-90',
          className,
        ),
      )}
    />
  );
}

interface ToggleFieldProps {
  label: string;
  description?: string;
  checked: boolean;
  disabled?: boolean;
  tooltip?: string;
  onChange: (checked: boolean) => void;
}

export function ToggleField({
  label,
  description,
  checked,
  disabled,
  tooltip,
  onChange,
}: ToggleFieldProps) {
  return (
    <label className="flex min-h-9 cursor-pointer items-start gap-2 self-start rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--surface-base)] px-2.5 py-2">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
        className="mt-0.5 h-3.5 w-3.5 rounded border-[var(--border-default)] accent-[var(--brand-primary)]"
      />
      <span className="flex flex-col">
        <span className="flex items-center gap-1.5 text-xs font-medium text-[var(--foreground-primary)]">
          <span>{label}</span>
          {tooltip ? <FieldTooltip content={tooltip} /> : null}
        </span>
        {description ? (
          <span className="mt-0.5 block text-[11px] leading-4 text-[var(--foreground-secondary)]">
            {description}
          </span>
        ) : null}
      </span>
    </label>
  );
}

export function CodeBlock({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <pre
      className={cn(
        'overflow-x-auto rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--surface-base)] px-2.5 py-2 text-[11px] leading-4 text-[var(--foreground-primary)]',
        className,
      )}
    >
      {children}
    </pre>
  );
}
