import { cn } from '@/lib/utils/cn';

const variantClasses = {
  success:
    'border-[var(--status-success-border)] bg-[var(--status-success-soft)] text-[var(--status-success-foreground)]',
  warning:
    'border-[var(--status-warning-border)] bg-[var(--status-warning-soft)] text-[var(--status-warning-foreground)]',
  danger:
    'border-[var(--status-danger-border)] bg-[var(--status-danger-soft)] text-[var(--status-danger-foreground)]',
  info: 'border-[var(--status-info-border)] bg-[var(--status-info-soft)] text-[var(--status-info-foreground)]',
} as const;

interface StatusBadgeProps {
  label: string;
  variant?: keyof typeof variantClasses;
  className?: string;
  onClick?: () => void;
  disabled?: boolean;
}

export function StatusBadge({
  label,
  variant = 'info',
  className,
  onClick,
  disabled = false,
}: StatusBadgeProps) {
  const badgeClassName = cn(
    'inline-flex min-h-5 items-center rounded-[4px] border px-1.5 py-0.5 text-[11px] font-medium leading-none',
    variantClasses[variant],
    onClick
      ? 'cursor-pointer transition hover:opacity-85 disabled:cursor-not-allowed disabled:opacity-60'
      : undefined,
    className,
  );

  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        disabled={disabled}
        className={badgeClassName}
      >
        {label}
      </button>
    );
  }

  return <span className={badgeClassName}>{label}</span>;
}
