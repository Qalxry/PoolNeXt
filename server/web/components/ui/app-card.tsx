import type { HTMLAttributes, ReactNode } from 'react';

import { cn } from '@/lib/utils/cn';

interface AppCardProps extends HTMLAttributes<HTMLDivElement> {
  title?: string;
  description?: string;
  action?: ReactNode;
}

export function AppCard({
  title,
  description,
  action,
  className,
  children,
  ...props
}: AppCardProps) {
  return (
    <section
      className={cn(
        'rounded-lg border border-[var(--border-default)] bg-[var(--surface-card)] shadow-[var(--shadow-soft)]',
        className,
      )}
      {...props}
    >
      {(title || description || action) && (
        <header className="flex flex-col gap-2 border-b border-[var(--border-default)] px-4 py-3 md:flex-row md:items-start md:justify-between">
          <div className="space-y-1">
            {title ? (
              <h2 className="text-sm font-semibold text-[var(--foreground-primary)]">
                {title}
              </h2>
            ) : null}
            {description ? (
              <p className="text-xs leading-5 text-[var(--foreground-secondary)]">
                {description}
              </p>
            ) : null}
          </div>
          {action ? <div className="shrink-0">{action}</div> : null}
        </header>
      )}
      <div className="px-4 py-4">{children}</div>
    </section>
  );
}
