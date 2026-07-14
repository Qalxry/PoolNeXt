import { cn } from '@/lib/utils/cn';

interface LoadingStateProps {
  className?: string;
}

export function LoadingState({ className }: LoadingStateProps) {
  return (
    <div className={cn('space-y-1.5', className)}>
      <div className="h-3 w-28 animate-pulse rounded bg-[var(--control-background-hover)]" />
      <div className="h-9 animate-pulse rounded-[var(--radius-control)] bg-[var(--surface-muted)]" />
      <div className="h-9 animate-pulse rounded-[var(--radius-control)] bg-[var(--surface-muted)]" />
    </div>
  );
}
