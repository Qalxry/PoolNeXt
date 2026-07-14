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
        'overflow-hidden rounded-[var(--radius-panel)] border border-[var(--border-default)] bg-[var(--surface-panel)] shadow-[var(--shadow-soft)]',
        className,
      )}
      {...props}
    >
      {(title || description || action) && (
        <header className="flex flex-col gap-1.5 border-b border-[var(--border-default)] bg-[var(--surface-base)] px-3 py-2 md:flex-row md:items-center md:justify-between">
          <div className="min-w-0">
            {title ? (
              <h2 className="text-[13px] font-semibold text-[var(--foreground-primary)]">
                {title}
              </h2>
            ) : null}
            {description ? (
              <p className="mt-0.5 text-[11px] leading-4 text-[var(--foreground-secondary)]">
                {description}
              </p>
            ) : null}
          </div>
          {action ? <div className="shrink-0">{action}</div> : null}
        </header>
      )}
      <div className="p-3">{children}</div>
    </section>
  );
}
