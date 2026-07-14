'use client';

import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useEffect, useMemo, useState } from 'react';

import { EmptyState } from '@/components/feedback/empty-state';
import { ErrorState } from '@/components/feedback/error-state';
import { InlineMessage } from '@/components/feedback/inline-message';
import { LoadingState } from '@/components/feedback/loading-state';
import { PageHeader } from '@/components/layout/page-header';
import { StatusBadge } from '@/components/ui/status-badge';
import { getLogs } from '@/features/logs/api/logs';
import {
  formatLogLine,
  getClassificationMeta,
  getLevelMeta,
  getSourceLabel,
  matchesLogKeyword,
} from '@/features/logs/lib/log-display';
import type {
  AppLogItem,
  LogClassification,
  LogLevel,
} from '@/features/logs/types';
import {
  PrimaryButton,
  ResourceInput,
  ResourceSelect,
  SecondaryButton,
} from '@/features/shared/components/resource-primitives';
import {
  formatDateTimeWithMilliseconds,
  formatRelativeTime,
} from '@/lib/utils/date';

const AUTO_REFRESH_INTERVAL_MS = 5000;
const CLIENT_LOG_LIMIT = 500;

type ClassificationFilter = LogClassification | 'all';
type LevelFilter = LogLevel | 'all';

function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : '日志请求失败，请稍后重试。';
}

function mergeLogs(existing: AppLogItem[], incoming: AppLogItem[]) {
  const byId = new Map(existing.map((item) => [item.id, item]));
  for (const item of incoming) {
    byId.set(item.id, item);
  }
  return Array.from(byId.values())
    .sort((left, right) => left.id - right.id)
    .slice(-CLIENT_LOG_LIMIT);
}

function CompactPanel({ children }: { children: ReactNode }) {
  return (
    <section className="rounded-xl border border-[var(--border-default)] bg-[var(--surface-card)] shadow-[var(--shadow-soft)]">
      {children}
    </section>
  );
}

function Metric({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="rounded-xl border border-[var(--border-default)] bg-[var(--surface-card)] px-4 py-3">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs text-[var(--foreground-secondary)]">
          {label}
        </span>
        <span className="text-xl font-semibold text-[var(--foreground-primary)] tabular-nums">
          {value}
        </span>
      </div>
    </div>
  );
}

export function LogsPage() {
  const [classification, setClassification] =
    useState<ClassificationFilter>('all');
  const [level, setLevel] = useState<LevelFilter>('all');
  const [source, setSource] = useState('all');
  const [search, setSearch] = useState('');
  const [logs, setLogs] = useState<AppLogItem[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [feedback, setFeedback] = useState('');

  const logsQuery = useQuery({
    queryKey: ['app-logs', classification, level, source],
    queryFn: () => getLogs({ classification, level, source, limit: 300 }),
  });

  useEffect(() => {
    if (logsQuery.data?.items) {
      setLogs(logsQuery.data.items.slice(-CLIENT_LOG_LIMIT));
    }
  }, [logsQuery.data]);

  const lastLogId = logs.at(-1)?.id ?? 0;

  useEffect(() => {
    if (isPaused) {
      return;
    }
    const timer = window.setInterval(async () => {
      try {
        const response = await getLogs({
          classification,
          level,
          source,
          afterId: lastLogId,
          limit: 100,
        });
        if (response.items.length > 0) {
          setLogs((current) => mergeLogs(current, response.items));
        }
      } catch {
        // A transient polling error should not replace the current log view.
      }
    }, AUTO_REFRESH_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [classification, isPaused, lastLogId, level, source]);

  const filteredLogs = useMemo(
    () => logs.filter((item) => matchesLogKeyword(item, search)),
    [logs, search],
  );
  const counts = useMemo(
    () => ({
      errors: filteredLogs.filter((item) => item.level === 'error').length,
      warnings: filteredLogs.filter((item) => item.level === 'warn').length,
      security: filteredLogs.filter(
        (item) => item.classification === 'security',
      ).length,
    }),
    [filteredLogs],
  );

  const handleManualRefresh = async () => {
    setFeedback('');
    try {
      const response = await getLogs({
        classification,
        level,
        source,
        afterId: lastLogId,
        limit: 100,
      });
      setLogs((current) => mergeLogs(current, response.items));
      setFeedback(
        response.items.length > 0
          ? `已同步 ${response.items.length} 条新日志。`
          : '当前没有新的日志。',
      );
    } catch (error) {
      setFeedback(getErrorMessage(error));
    }
  };

  const handleCopy = async () => {
    const content = filteredLogs.map(formatLogLine).join('\n');
    if (!content) {
      setFeedback('当前没有可复制的日志。');
      return;
    }
    await navigator.clipboard.writeText(content);
    setFeedback('结构化日志已复制。');
  };

  const handleDownload = () => {
    if (filteredLogs.length === 0) {
      setFeedback('当前没有可导出的日志。');
      return;
    }
    const content = filteredLogs.map((item) => JSON.stringify(item)).join('\n');
    const blob = new Blob([content], {
      type: 'application/x-ndjson;charset=utf-8',
    });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `poolnext-logs-${Date.now()}.jsonl`;
    link.click();
    URL.revokeObjectURL(url);
    setFeedback('JSONL 日志文件已生成。');
  };

  if (logsQuery.isLoading) {
    return <LoadingState />;
  }
  if (logsQuery.isError) {
    return (
      <ErrorState
        title="日志加载失败"
        description={getErrorMessage(logsQuery.error)}
      />
    );
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="事件日志"
        description="日志按事件、级别和关键字段展示；数据库最多保留 5000 条 / 30 天，页面内存最多保留 500 条。"
        action={
          <div className="flex flex-wrap gap-2">
            <SecondaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={() => setIsPaused((value) => !value)}
            >
              {isPaused ? '继续同步' : '暂停同步'}
            </SecondaryButton>
            <SecondaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={() => void handleCopy()}
            >
              复制
            </SecondaryButton>
            <SecondaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={handleDownload}
            >
              导出 JSONL
            </SecondaryButton>
            <PrimaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={() => void handleManualRefresh()}
            >
              立即同步
            </PrimaryButton>
          </div>
        }
      />

      {feedback ? <InlineMessage tone="info" message={feedback} /> : null}

      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <Metric label="当前结果" value={filteredLogs.length} />
        <Metric label="错误" value={counts.errors} />
        <Metric label="警告" value={counts.warnings} />
        <Metric label="安全事件" value={counts.security} />
      </div>

      <CompactPanel>
        <div className="grid gap-2 p-3 lg:grid-cols-[10rem_9rem_9rem_minmax(16rem,1fr)]">
          <ResourceSelect
            value={classification}
            onChange={(event) =>
              setClassification(event.target.value as ClassificationFilter)
            }
            className="!rounded-xl !px-3 !py-2"
            aria-label="日志分类"
          >
            <option value="all">全部分类</option>
            <option value="system">系统</option>
            <option value="business">业务</option>
            <option value="security">安全</option>
          </ResourceSelect>
          <ResourceSelect
            value={level}
            onChange={(event) => setLevel(event.target.value as LevelFilter)}
            className="!rounded-xl !px-3 !py-2"
            aria-label="日志级别"
          >
            <option value="all">全部级别</option>
            <option value="error">ERROR</option>
            <option value="warn">WARN</option>
            <option value="info">INFO</option>
            <option value="debug">DEBUG</option>
          </ResourceSelect>
          <ResourceSelect
            value={source}
            onChange={(event) => setSource(event.target.value)}
            className="!rounded-xl !px-3 !py-2"
            aria-label="日志来源"
          >
            <option value="all">全部来源</option>
            <option value="backend">后端</option>
            <option value="frontend">前端</option>
            <option value="scheduler">调度器</option>
            <option value="mihomo">Mihomo</option>
          </ResourceSelect>
          <ResourceInput
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="!rounded-xl !px-3 !py-2"
            placeholder="搜索消息、事件名、用户、节点 ID 或错误原因"
            aria-label="搜索日志"
          />
        </div>
      </CompactPanel>

      <CompactPanel>
        <div className="flex items-center justify-between border-b border-[var(--border-default)] px-3 py-2 text-xs text-[var(--foreground-secondary)]">
          <span>{isPaused ? '同步已暂停' : '每 5 秒增量同步'}</span>
          <span>{lastLogId > 0 ? `最新 ID ${lastLogId}` : '暂无日志'}</span>
        </div>
        {filteredLogs.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="暂无匹配日志"
              description="调整分类、级别、来源或搜索条件。"
            />
          </div>
        ) : (
          <div className="max-h-[680px] overflow-y-auto">
            {filteredLogs
              .slice()
              .reverse()
              .map((item) => {
                const classificationMeta = getClassificationMeta(
                  item.classification,
                );
                const levelMeta = getLevelMeta(item.level);
                const details = Object.entries(item.details ?? {});
                return (
                  <article
                    key={item.id}
                    className="grid gap-2 border-b border-[var(--border-default)] px-3 py-3 last:border-b-0 hover:bg-[var(--surface-muted)]/60 lg:grid-cols-[8.5rem_9rem_minmax(0,1fr)]"
                  >
                    <div
                      className="text-[11px] leading-5 text-[var(--foreground-muted)]"
                      title={formatDateTimeWithMilliseconds(item.created_at)}
                    >
                      <p>{formatRelativeTime(item.created_at)}</p>
                      <p className="font-mono">#{item.id}</p>
                    </div>
                    <div className="flex flex-wrap content-start gap-1.5">
                      <StatusBadge
                        label={levelMeta.label}
                        variant={levelMeta.variant}
                        className="!px-2 !py-0.5"
                      />
                      <StatusBadge
                        label={classificationMeta.label}
                        variant={classificationMeta.variant}
                        className="!px-2 !py-0.5"
                      />
                    </div>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                        <p className="font-medium text-[var(--foreground-primary)]">
                          {item.message}
                        </p>
                        <span className="text-[10px] tracking-wide text-[var(--foreground-muted)] uppercase">
                          {getSourceLabel(item.source)} ·{' '}
                          {item.event || 'general'}
                        </span>
                      </div>
                      {details.length > 0 ? (
                        <dl className="mt-2 flex flex-wrap gap-1.5">
                          {details.map(([key, value]) => (
                            <div
                              key={key}
                              className="inline-flex max-w-full gap-1 rounded-md border border-[var(--border-default)] bg-[var(--surface-elevated)] px-2 py-1 font-mono text-[10px]"
                              title={`${key}=${value}`}
                            >
                              <dt className="text-[var(--foreground-muted)]">
                                {key}
                              </dt>
                              <dd className="max-w-80 truncate text-[var(--foreground-secondary)]">
                                {value}
                              </dd>
                            </div>
                          ))}
                        </dl>
                      ) : null}
                    </div>
                  </article>
                );
              })}
          </div>
        )}
      </CompactPanel>
    </div>
  );
}
