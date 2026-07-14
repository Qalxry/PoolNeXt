import Link from 'next/link';

import { AppCard } from '@/components/ui/app-card';

export default function NotFound() {
  return (
    <main className="flex min-h-screen items-center justify-center px-4">
      <AppCard title="页面不存在">
        <div className="space-y-2 text-xs leading-4 text-[var(--foreground-secondary)]">
          <p>请检查地址或返回内核页。</p>
          <Link
            href="/"
            className="inline-flex h-8 items-center rounded-[var(--radius-control)] bg-[var(--brand-primary)] px-3 font-medium text-[var(--foreground-inverse)] transition hover:opacity-90"
          >
            返回总览
          </Link>
        </div>
      </AppCard>
    </main>
  );
}
