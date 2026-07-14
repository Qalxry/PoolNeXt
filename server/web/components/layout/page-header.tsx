import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: string;
  description?: string;
  action?: ReactNode;
}

export function PageHeader({ title, description, action }: PageHeaderProps) {
  return (
    <div className="flex min-h-10 flex-col gap-2 border-b border-[var(--border-default)] pb-2 md:flex-row md:items-center md:justify-between">
      <div className="min-w-0">
        <h1 className="text-lg font-semibold text-[var(--foreground-primary)]">
          {title}
        </h1>
        {description ? (
          <p className="mt-0.5 max-w-5xl text-xs leading-4 text-[var(--foreground-secondary)]">
            {description}
          </p>
        ) : null}
      </div>
      {action ? (
        <div className="flex shrink-0 flex-wrap items-center gap-1.5">
          {action}
        </div>
      ) : null}
    </div>
  );
}
