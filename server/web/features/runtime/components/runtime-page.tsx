'use client';

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ExternalLink,
  Pause,
  Play,
  RefreshCw,
  RotateCw,
  Square,
} from 'lucide-react';

import { EmptyState } from '@/components/feedback/empty-state';
import { ErrorState } from '@/components/feedback/error-state';
import { InlineMessage } from '@/components/feedback/inline-message';
import { LoadingState } from '@/components/feedback/loading-state';
import { PageHeader } from '@/components/layout/page-header';
import { AppCard } from '@/components/ui/app-card';
import { StatusBadge } from '@/components/ui/status-badge';
import { getKernelCapability } from '@/features/capability/api/capability';
import { ConnectionLedger } from '@/features/runtime/components/connection-ledger';
import {
  getRuntimeConnections,
  getRuntimeLogs,
  getRuntimeStatus,
  reloadRuntime,
  startRuntime,
  stopRuntime,
} from '@/features/runtime/api/runtime';
import type { RuntimeLogItem } from '@/features/runtime/types';
import {
  DangerButton,
  PrimaryButton,
  SecondaryButton,
} from '@/features/shared/components/resource-primitives';
import { formatDateTime } from '@/lib/utils/date';

const runtimeStatusQueryKey = ['runtime', 'status'] as const;

type FeedbackState = {
  tone: 'success' | 'danger' | 'info';
  message: string;
};

function getErrorMessage(error: unknown) {
  return error instanceof Error
    ? error.message
    : '运行控制请求失败，请稍后重试。';
}

function mergeLogs(existing: RuntimeLogItem[], incoming: RuntimeLogItem[]) {
  if (incoming.length === 0) {
    return existing;
  }
  const result = [...existing];
  const seen = new Set(existing.map((item) => item.seq));
  for (const item of incoming) {
    if (!seen.has(item.seq)) {
      result.push(item);
    }
  }
  return result.slice(-300);
}

function runtimeLogLevelMeta(level: string) {
  switch (level.toLowerCase()) {
    case 'error':
    case 'fatal':
      return { label: level.toUpperCase(), variant: 'danger' as const };
    case 'warn':
    case 'warning':
      return { label: 'WARN', variant: 'warning' as const };
    case 'debug':
      return { label: 'DEBUG', variant: 'info' as const };
    default:
      return { label: 'INFO', variant: 'success' as const };
  }
}

function RuntimeMetric({
  label,
  value,
  detail,
}: {
  label: string;
  value: string | number;
  detail?: string;
}) {
  return (
    <div className="flex min-h-11 items-center justify-between gap-2 px-2.5 py-1.5">
      <div className="min-w-0">
        <span className="block text-[10px] text-[var(--foreground-secondary)]">
          {label}
        </span>
        {detail ? (
          <span
            className="block max-w-44 truncate text-[9px] text-[var(--foreground-muted)]"
            title={detail}
          >
            {detail}
          </span>
        ) : null}
      </div>
      <span className="shrink-0 text-sm font-semibold text-[var(--foreground-primary)] tabular-nums">
        {value}
      </span>
    </div>
  );
}

export function RuntimePage() {
  const queryClient = useQueryClient();
  const [feedback, setFeedback] = useState<FeedbackState | null>(null);
  const [logs, setLogs] = useState<RuntimeLogItem[]>([]);
  const [autoRefresh, setAutoRefresh] = useState(true);

  const capabilityQuery = useQuery({
    queryKey: ['capability'],
    queryFn: getKernelCapability,
  });
  const statusQuery = useQuery({
    queryKey: runtimeStatusQueryKey,
    queryFn: getRuntimeStatus,
    refetchInterval: autoRefresh ? 5000 : false,
  });

  const logsQuery = useQuery({
    queryKey: ['runtime', 'logs', 0],
    queryFn: () => getRuntimeLogs(0, 100),
  });
  const connectionsQuery = useQuery({
    queryKey: ['runtime', 'connections'],
    queryFn: getRuntimeConnections,
    refetchInterval: autoRefresh ? 2000 : false,
  });

  useEffect(() => {
    if (logsQuery.data?.items) {
      setLogs(logsQuery.data.items);
    }
  }, [logsQuery.data]);

  const lastSeq = logs.length > 0 ? logs[logs.length - 1].seq : 0;

  useEffect(() => {
    if (!autoRefresh) {
      return;
    }
    const timer = window.setInterval(async () => {
      try {
        const response = await getRuntimeLogs(lastSeq, 100);
        if (response.items.length > 0) {
          setLogs((current) => mergeLogs(current, response.items));
        }
      } catch {
        // keep runtime page usable during transient polling failures
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [autoRefresh, lastSeq]);

  const startMutation = useMutation({
    mutationFn: startRuntime,
    onSuccess: async () => {
      setFeedback({ tone: 'success', message: 'Mihomo 已启动。' });
      await queryClient.invalidateQueries({ queryKey: runtimeStatusQueryKey });
      const response = await getRuntimeLogs(0, 100);
      setLogs(response.items);
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const stopMutation = useMutation({
    mutationFn: stopRuntime,
    onSuccess: async () => {
      setFeedback({ tone: 'success', message: 'Mihomo 已停止。' });
      await queryClient.invalidateQueries({ queryKey: runtimeStatusQueryKey });
    },
    onError: async (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
      // A stale PID is cleared by the backend before it reports the safety
      // refusal. Refresh immediately so the user can start the runtime again.
      await queryClient.invalidateQueries({ queryKey: runtimeStatusQueryKey });
    },
  });

  const reloadMutation = useMutation({
    mutationFn: reloadRuntime,
    onSuccess: async () => {
      setFeedback({ tone: 'success', message: 'Mihomo 已执行热重载。' });
      await queryClient.invalidateQueries({ queryKey: runtimeStatusQueryKey });
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const status = statusQuery.data;
  const capability = capabilityQuery.data;
  const hasStalePID = Boolean(status?.instance.pid) && !status?.running;
  const listenerSummary = useMemo(
    () => status?.listeners ?? [],
    [status?.listeners],
  );

  if (statusQuery.isLoading) {
    return <LoadingState />;
  }

  if (statusQuery.isError || !status) {
    return (
      <ErrorState
        title="运行状态加载失败"
        description={getErrorMessage(statusQuery.error)}
      />
    );
  }

  return (
    <div className="space-y-3">
      <PageHeader
        title="内核"
        action={
          <div className="flex flex-wrap gap-1.5">
            <SecondaryButton
              type="button"
              onClick={() => setAutoRefresh((value) => !value)}
            >
              {autoRefresh ? (
                <Pause className="h-3.5 w-3.5" aria-hidden="true" />
              ) : (
                <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
              )}
              {autoRefresh ? '暂停' : '刷新'}
            </SecondaryButton>
            <PrimaryButton
              type="button"
              onClick={() => {
                setFeedback(null);
                startMutation.mutate();
              }}
              disabled={
                startMutation.isPending ||
                status.running ||
                !capability?.supports_start
              }
            >
              <Play className="h-3.5 w-3.5" aria-hidden="true" />
              {startMutation.isPending ? '启动中...' : '启动'}
            </PrimaryButton>
            <SecondaryButton
              type="button"
              onClick={() => {
                setFeedback(null);
                reloadMutation.mutate();
              }}
              disabled={
                reloadMutation.isPending ||
                !status.running ||
                !capability?.supports_reload
              }
            >
              <RotateCw className="h-3.5 w-3.5" aria-hidden="true" />
              {reloadMutation.isPending ? '重载中...' : '热重载'}
            </SecondaryButton>
            <DangerButton
              type="button"
              onClick={() => {
                setFeedback(null);
                stopMutation.mutate();
              }}
              disabled={
                stopMutation.isPending || (!status.running && !hasStalePID)
              }
            >
              <Square className="h-3.5 w-3.5" aria-hidden="true" />
              {stopMutation.isPending
                ? '处理中...'
                : hasStalePID
                  ? '清理历史状态'
                  : '停止'}
            </DangerButton>
            <SecondaryButton
              type="button"
              onClick={() =>
                window.open('/zashboard/', '_blank', 'noopener,noreferrer')
              }
              disabled={!status.running}
            >
              <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
              Clash 控制台
            </SecondaryButton>
          </div>
        }
      />

      {feedback ? (
        <InlineMessage tone={feedback.tone} message={feedback.message} />
      ) : null}
      {capability ? (
        <InlineMessage
          tone={capability.binary_exists ? 'info' : 'danger'}
          message={capability.message}
        />
      ) : null}
      {hasStalePID ? (
        <InlineMessage
          tone="danger"
          message={`检测到未由当前 PoolNeXt 进程托管的历史 PID ${status.instance.pid}。请点击“清理历史状态”后再启动。`}
        />
      ) : null}
      {!status.config_valid ? (
        <InlineMessage
          tone="danger"
          message={`当前编排配置无效：${status.config_error || '请检查策略组和分流规则。'}`}
        />
      ) : status.config_pending ? (
        <InlineMessage
          tone="info"
          message="编排配置有未应用改动，请在编排页执行“应用并重载”。"
        />
      ) : null}

      <div className="metric-strip grid-cols-2 md:grid-cols-3 xl:grid-cols-5">
        <RuntimeMetric
          label="进程"
          value={status.running ? '运行中' : '已停止'}
          detail={status.instance.status}
        />
        <RuntimeMetric
          label="自动启动"
          value={status.auto_start_enabled ? '已开启' : '已关闭'}
          detail={
            status.instance.last_action === 'auto_start'
              ? status.instance.last_error || '最近启动成功'
              : undefined
          }
        />
        <RuntimeMetric
          label="控制 API"
          value={status.api_healthy ? '正常' : '未就绪'}
          detail={status.api_version || status.instance.controller_address}
        />
        <RuntimeMetric label="端口配置" value={status.profile_count} />
        <RuntimeMetric label="监听入口" value={status.listener_count} />
      </div>

      <AppCard title="实例详情">
        <div className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[11px] md:grid-cols-[6rem_minmax(0,1fr)_6rem_minmax(0,1fr)]">
          <span className="text-[var(--foreground-muted)]">PID</span>
          <span className="text-[var(--foreground-primary)] tabular-nums">
            {status.instance.pid ?? '未运行'}
          </span>
          <span className="text-[var(--foreground-muted)]">最近动作</span>
          <span className="text-[var(--foreground-primary)]">
            {status.instance.last_action || '无'}
          </span>
          <span className="text-[var(--foreground-muted)]">启动时间</span>
          <span className="text-[var(--foreground-secondary)] tabular-nums">
            {status.instance.last_started_at
              ? formatDateTime(status.instance.last_started_at)
              : '未启动'}
          </span>
          <span className="text-[var(--foreground-muted)]">重载时间</span>
          <span className="text-[var(--foreground-secondary)] tabular-nums">
            {status.instance.last_reloaded_at
              ? formatDateTime(status.instance.last_reloaded_at)
              : '未重载'}
          </span>
          <span className="text-[var(--foreground-muted)]">运行目录</span>
          <span
            className="truncate font-mono text-[10px] text-[var(--foreground-secondary)]"
            title={status.instance.work_dir}
          >
            {status.instance.work_dir || '未生成'}
          </span>
          <span className="text-[var(--foreground-muted)]">配置路径</span>
          <span
            className="truncate font-mono text-[10px] text-[var(--foreground-secondary)]"
            title={status.instance.config_path}
          >
            {status.instance.config_path || '未生成'}
          </span>
          <span className="text-[var(--foreground-muted)]">控制地址</span>
          <span className="font-mono text-[10px] text-[var(--foreground-secondary)]">
            {status.instance.controller_address || '未生成'}
          </span>
          <span className="text-[var(--foreground-muted)]">最近错误</span>
          <span
            className="truncate text-[var(--status-danger-foreground)]"
            title={status.instance.last_error}
          >
            {status.instance.last_error || '无'}
          </span>
        </div>
      </AppCard>

      <AppCard title="监听列表">
        {listenerSummary.length === 0 ? (
          <EmptyState
            title="暂无监听入口"
            description="请先在工作台启用至少一个端口配置。"
          />
        ) : (
          <div className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--border-default)]">
            <table className="w-full min-w-[720px] border-collapse text-left text-[11px]">
              <thead className="bg-[var(--surface-muted)] text-[10px] text-[var(--foreground-secondary)]">
                <tr>
                  <th className="px-2 py-1.5 font-medium">入口</th>
                  <th className="w-20 px-2 py-1.5 font-medium">协议</th>
                  <th className="min-w-48 px-2 py-1.5 font-medium">监听地址</th>
                  <th className="min-w-48 px-2 py-1.5 font-medium">工作台</th>
                  <th className="min-w-48 px-2 py-1.5 font-medium">策略组</th>
                </tr>
              </thead>
              <tbody>
                {listenerSummary.map((listener) => (
                  <tr
                    key={listener.name}
                    className="border-t border-[var(--border-default)] hover:bg-[var(--surface-muted)]/70"
                  >
                    <td className="h-8 px-2 py-0 font-medium text-[var(--foreground-primary)]">
                      {listener.name}
                    </td>
                    <td className="h-8 px-2 py-0 text-[var(--foreground-secondary)] uppercase">
                      {listener.type}
                    </td>
                    <td className="h-8 px-2 py-0 font-mono text-[10px] text-[var(--foreground-secondary)]">
                      {listener.listen}:{listener.port}
                    </td>
                    <td className="h-8 px-2 py-0 text-[var(--foreground-secondary)]">
                      {listener.profile_name}
                    </td>
                    <td className="h-8 px-2 py-0 text-[var(--foreground-secondary)]">
                      {listener.entry_mode === 'rule'
                        ? '规则分流'
                        : listener.proxy_group_name}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </AppCard>

      <AppCard title="最终配置摘要">
        <div className="grid gap-2 text-[11px] text-[var(--foreground-secondary)] md:grid-cols-3">
          <div>
            <p className="text-xs text-[var(--foreground-muted)]">活动配置</p>
            <p className="font-medium text-[var(--foreground-primary)]">
              {status.instance.active_config_checksum ? '已加载' : '未加载'}
            </p>
          </div>
          <div>
            <p className="text-xs text-[var(--foreground-muted)]">参与工作台</p>
            <p className="font-medium text-[var(--foreground-primary)]">
              {status.profile_count}
            </p>
          </div>
          <div>
            <p className="text-xs text-[var(--foreground-muted)]">监听入口</p>
            <p className="font-medium text-[var(--foreground-primary)]">
              {status.listener_count}
            </p>
          </div>
          <div className="md:col-span-3">
            <p className="text-xs text-[var(--foreground-muted)]">
              活动配置校验和
            </p>
            <p className="font-mono text-[10px] break-all text-[var(--foreground-primary)]">
              {status.instance.active_config_checksum || '暂无'}
            </p>
          </div>
        </div>
      </AppCard>

      <ConnectionLedger
        snapshot={connectionsQuery.data}
        running={status.running}
      />

      <details className="overflow-hidden rounded-[var(--radius-panel)] border border-[var(--border-default)] bg-[var(--surface-panel)] shadow-[var(--shadow-soft)]">
        <summary className="flex cursor-pointer list-none items-center justify-between border-b border-[var(--border-default)] bg-[var(--surface-base)] px-3 py-2">
          <span className="text-[13px] font-semibold text-[var(--foreground-primary)]">
            原始内核输出
          </span>
          <span className="text-[10px] text-[var(--foreground-muted)]">
            {logs.length} 条{lastSeq > 0 ? ` · 最新 #${lastSeq}` : ''}
          </span>
        </summary>
        <div className="p-3">
          {logsQuery.isLoading ? <LoadingState /> : null}
          {logs.length === 0 ? (
            <EmptyState
              title="暂无运行日志"
              description="启动 Mihomo 后，这里会持续展示 stdout 和 stderr 输出。"
            />
          ) : (
            <div className="max-h-[420px] overflow-auto rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--surface-base)]">
              {logs
                .slice()
                .reverse()
                .map((item) => {
                  const level = runtimeLogLevelMeta(item.level);
                  return (
                    <div
                      key={item.seq}
                      className="grid h-8 min-w-[760px] grid-cols-[10rem_7rem_minmax(0,1fr)] items-center gap-2 border-b border-[var(--border-default)] px-2 last:border-b-0 hover:bg-[var(--surface-muted)]"
                    >
                      <time className="truncate font-mono text-[10px] text-[var(--foreground-muted)]">
                        {formatDateTime(item.created_at)}
                      </time>
                      <div className="flex items-center gap-1">
                        <StatusBadge
                          label={level.label}
                          variant={level.variant}
                          className="w-fit"
                        />
                        <span className="text-[10px] text-[var(--foreground-muted)] uppercase">
                          {item.stream}
                        </span>
                      </div>
                      <p
                        className="min-w-0 truncate font-mono text-[10px] text-[var(--foreground-primary)]"
                        title={item.message}
                      >
                        {item.message}
                      </p>
                    </div>
                  );
                })}
            </div>
          )}
        </div>
      </details>
    </div>
  );
}
