interface ErrorStateProps {
  title: string;
  description: string;
}

export function ErrorState({ title, description }: ErrorStateProps) {
  return (
    <div className="rounded-[var(--radius-panel)] border border-[var(--status-danger-border)] bg-[var(--status-danger-soft)] px-3 py-3 text-xs">
      <p className="font-semibold text-[var(--status-danger-foreground)]">
        {title}
      </p>
      <p className="mt-1 leading-4 text-[var(--status-danger-foreground)]/80">
        {description}
      </p>
    </div>
  );
}
