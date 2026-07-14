import Link from 'next/link';
import type { ReactNode } from 'react';

import { publicEnv } from '@/lib/env/public-env';

interface PublicShellProps {
  children: ReactNode;
}

export function PublicShell({ children }: PublicShellProps) {
  return (
    <div className="flex min-h-screen items-center justify-center px-3 py-6">
      <div className="w-full max-w-3xl">
        <div className="mb-3 flex h-9 items-center justify-between border-b border-[var(--border-default)] pb-3">
          <p className="text-lg font-semibold text-[var(--foreground-primary)]">
            PoolNeXt
          </p>
          <div className="text-[11px] text-[var(--foreground-secondary)]">
            <span className="rounded-[4px] border border-[var(--border-default)] px-1.5 py-0.5">
              {publicEnv.appVersion}
            </span>
          </div>
        </div>

        <div className="space-y-3">{children}</div>

        <div className="mt-3 border-t border-[var(--border-default)] pt-3 text-xs text-[var(--foreground-secondary)]">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/"
              className="text-[var(--brand-primary)] transition hover:opacity-80"
            >
              返回
            </Link>
            <Link
              href="/about"
              className="text-[var(--brand-primary)] transition hover:opacity-80"
            >
              关于
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
