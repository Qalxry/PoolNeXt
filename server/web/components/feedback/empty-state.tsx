interface EmptyStateProps {
  title: string;
  description?: string;
}

export function EmptyState({ title, description }: EmptyStateProps) {
  return (
    <div className="rounded-[var(--radius-panel)] border border-dashed border-[var(--border-default)] bg-[var(--surface-base)] px-3 py-4 text-xs">
      <p className="font-semibold text-[var(--foreground-primary)]">{title}</p>
      {description ? (
        <p className="mt-1 leading-4 text-[var(--foreground-secondary)]">
          {description}
        </p>
      ) : null}
    </div>
  );
}
