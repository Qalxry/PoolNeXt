'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';

import { EmptyState } from '@/components/feedback/empty-state';
import { ErrorState } from '@/components/feedback/error-state';
import { LoadingState } from '@/components/feedback/loading-state';
import { AppCard } from '@/components/ui/app-card';
import { StatusBadge } from '@/components/ui/status-badge';
import { getPublicStatus } from '@/features/auth/api/public';
import { getAboutContent } from '@/features/settings/api/settings';
import { formatDateTime } from '@/lib/utils/date';
import { renderSafeMarkdown } from '@/lib/security/safe-markdown';

function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试。';
}

export function AboutPage() {
  const aboutQuery = useQuery({
    queryKey: ['about'],
    queryFn: getAboutContent,
  });

  const statusQuery = useQuery({
    queryKey: ['public-status'],
    queryFn: getPublicStatus,
  });

  if (aboutQuery.isLoading || statusQuery.isLoading) {
    return <LoadingState />;
  }

  if (aboutQuery.isError) {
    return (
      <ErrorState
        title="关于页加载失败"
        description={getErrorMessage(aboutQuery.error)}
      />
    );
  }

  if (statusQuery.isError) {
    return (
      <ErrorState
        title="系统状态加载失败"
        description={getErrorMessage(statusQuery.error)}
      />
    );
  }

  const aboutContent = aboutQuery.data?.trim() ?? '';
  const status = statusQuery.data;

  if (!status) {
    return (
      <EmptyState
        title="暂无系统信息"
        description="未能获取当前系统的公开状态信息。"
      />
    );
  }

  return (
    <div className="space-y-3">
      <AppCard
        title="关于 PoolNeXt"
        action={<StatusBadge label={status.version || 'dev'} variant="info" />}
      >
        <div className="metric-strip grid-cols-1 md:grid-cols-3">
          <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
            <span className="text-[10px] text-[var(--foreground-muted)]">
              系统名称
            </span>
            <span className="text-xs font-medium text-[var(--foreground-primary)]">
              {status.system_name || 'PoolNeXt'}
            </span>
          </div>
          <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
            <span className="text-[10px] text-[var(--foreground-muted)]">
              启动时间
            </span>
            <span className="text-[11px] font-medium text-[var(--foreground-primary)] tabular-nums">
              {formatDateTime(new Date(status.start_time * 1000))}
            </span>
          </div>
          <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
            <span className="text-[10px] text-[var(--foreground-muted)]">
              项目仓库
            </span>
            <a
              href="https://github.com/Qalxry/PoolNeXt"
              target="_blank"
              rel="noreferrer"
              className="text-xs font-medium text-[var(--brand-primary)] transition hover:opacity-80"
            >
              项目仓库
            </a>
          </div>
        </div>
      </AppCard>

      {aboutContent ? (
        <AppCard title="项目介绍">
          <div
            className="prose prose-sm max-w-none text-[var(--foreground-primary)] [&_a]:text-[var(--brand-primary)]"
            dangerouslySetInnerHTML={{
              __html: renderSafeMarkdown(aboutContent),
            }}
          />
        </AppCard>
      ) : (
        <EmptyState
          title="尚未配置关于内容"
          description="可在设置页中维护关于内容，这里会自动同步展示。"
        />
      )}

      <div className="flex flex-wrap gap-3 text-sm">
        <Link
          href="/login"
          className="text-[var(--brand-primary)] transition hover:opacity-80"
        >
          前往登录
        </Link>
      </div>
    </div>
  );
}
