'use client';

import { useQuery } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import {
  ChevronRight,
  Clipboard,
  Download,
  Pause,
  Play,
  RefreshCw,
} from 'lucide-react';

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
import { formatDateTimeWithMilliseconds } from '@/lib/utils/date';

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

function Metric({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="flex min-h-10 items-center justify-between gap-3 px-3 py-1.5">
      <span className="text-[11px] text-[var(--foreground-secondary)]">
        {label}
      </span>
      <span className="text-sm font-semibold text-[var(--foreground-primary)] tabular-nums">
        {value}
      </span>
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
    <div className="space-y-3">
      <PageHeader
        title="事件日志"
        action={
          <div className="flex flex-wrap gap-1.5">
            <SecondaryButton
              type="button"
              onClick={() => setIsPaused((value) => !value)}
            >
              {isPaused ? (
                <Play className="h-3.5 w-3.5" aria-hidden="true" />
              ) : (
                <Pause className="h-3.5 w-3.5" aria-hidden="true" />
              )}
              {isPaused ? '继续' : '暂停'}
            </SecondaryButton>
            <SecondaryButton type="button" onClick={() => void handleCopy()}>
              <Clipboard className="h-3.5 w-3.5" aria-hidden="true" />
              复制
            </SecondaryButton>
            <SecondaryButton type="button" onClick={handleDownload}>
              <Download className="h-3.5 w-3.5" aria-hidden="true" />
              导出
            </SecondaryButton>
            <PrimaryButton
              type="button"
              onClick={() => void handleManualRefresh()}
            >
              <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
              同步
            </PrimaryButton>
          </div>
        }
      />

      {feedback ? <InlineMessage tone="info" message={feedback} /> : null}

      <div className="metric-strip grid-cols-2 md:grid-cols-4">
        <Metric label="当前结果" value={filteredLogs.length} />
        <Metric label="错误" value={counts.errors} />
        <Metric label="警告" value={counts.warnings} />
        <Metric label="安全事件" value={counts.security} />
      </div>

      <section className="app-toolbar grid lg:grid-cols-[8.5rem_8rem_8rem_minmax(16rem,1fr)]">
        <ResourceSelect
          value={classification}
          onChange={(event) =>
            setClassification(event.target.value as ClassificationFilter)
          }
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
          placeholder="搜索消息、事件、用户、节点或错误"
          aria-label="搜索日志"
        />
      </section>

      <section className="data-grid-shell">
        <div className="flex h-8 items-center justify-between border-b border-[var(--border-default)] bg-[var(--surface-base)] px-2.5 text-[11px] text-[var(--foreground-secondary)]">
          <span>{isPaused ? '同步已暂停' : '每 5 秒同步'}</span>
          <span className="tabular-nums">
            {lastLogId > 0
              ? `${filteredLogs.length} 条 · 最新 #${lastLogId}`
              : '暂无日志'}
          </span>
        </div>
        {filteredLogs.length === 0 ? (
          <div className="p-3">
            <EmptyState
              title="暂无匹配日志"
              description="调整分类、级别、来源或搜索条件。"
            />
          </div>
        ) : (
          <div className="max-h-[calc(100vh-260px)] overflow-auto">
            <div className="sticky top-0 z-10 grid min-w-[940px] grid-cols-[1.1rem_9.5rem_4.25rem_4.5rem_5.25rem_10rem_minmax(20rem,1fr)] items-center gap-2 border-b border-[var(--border-default)] bg-[var(--surface-muted)] px-2 py-1 text-[10px] font-medium text-[var(--foreground-muted)] uppercase">
              <span />
              <span>时间 / ID</span>
              <span>级别</span>
              <span>分类</span>
              <span>来源</span>
              <span>事件</span>
              <span>摘要</span>
            </div>
            {filteredLogs
              .slice()
              .reverse()
              .map((item) => {
                const classificationMeta = getClassificationMeta(
                  item.classification,
                );
                const levelMeta = getLevelMeta(item.level);
                const details = Object.entries(item.details ?? {});
                const exactTime = formatDateTimeWithMilliseconds(
                  item.created_at,
                );
                return (
                  <details
                    key={item.id}
                    className="group min-w-[940px] border-b border-[var(--border-default)] last:border-b-0 open:bg-[var(--surface-base)]"
                  >
                    <summary className="grid h-8 cursor-pointer list-none grid-cols-[1.1rem_9.5rem_4.25rem_4.5rem_5.25rem_10rem_minmax(20rem,1fr)] items-center gap-2 px-2 text-[11px] hover:bg-[var(--surface-muted)] [&::-webkit-details-marker]:hidden">
                      <ChevronRight
                        className="h-3 w-3 text-[var(--foreground-muted)] transition-transform group-open:rotate-90"
                        aria-hidden="true"
                      />
                      <span
                        className="truncate font-mono text-[10px] text-[var(--foreground-muted)] tabular-nums"
                        title={exactTime}
                      >
                        {exactTime.slice(11)} · #{item.id}
                      </span>
                      <StatusBadge
                        label={levelMeta.label}
                        variant={levelMeta.variant}
                        className="w-fit"
                      />
                      <StatusBadge
                        label={classificationMeta.label.replace('日志', '')}
                        variant={classificationMeta.variant}
                        className="w-fit"
                      />
                      <span className="truncate text-[var(--foreground-secondary)]">
                        {getSourceLabel(item.source)}
                      </span>
                      <span
                        className="truncate font-mono text-[10px] text-[var(--foreground-secondary)]"
                        title={item.event || 'general'}
                      >
                        {item.event || 'general'}
                      </span>
                      <span
                        className="truncate font-medium text-[var(--foreground-primary)]"
                        title={item.message}
                      >
                        {item.message}
                      </span>
                    </summary>
                    <div className="grid grid-cols-[9rem_minmax(0,1fr)] gap-x-3 gap-y-2 border-t border-[var(--border-default)] bg-[var(--surface-base)] px-7 py-2 text-[11px]">
                      <span className="text-[var(--foreground-muted)]">
                        准确时间
                      </span>
                      <span className="font-mono text-[var(--foreground-secondary)]">
                        {exactTime}
                      </span>
                      <span className="text-[var(--foreground-muted)]">
                        事件摘要
                      </span>
                      <span className="text-[var(--foreground-primary)]">
                        {item.message}
                      </span>
                      {details.length > 0 ? (
                        <>
                          <span className="text-[var(--foreground-muted)]">
                            结构化详情
                          </span>
                          <dl className="flex min-w-0 flex-wrap gap-1">
                            {details.map(([key, value]) => (
                              <div
                                key={key}
                                className="inline-flex max-w-full gap-1 rounded-[4px] border border-[var(--border-default)] bg-[var(--surface-panel)] px-1.5 py-0.5 font-mono text-[10px]"
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
                        </>
                      ) : null}
                    </div>
                  </details>
                );
              })}
          </div>
        )}
      </section>
    </div>
  );
}
