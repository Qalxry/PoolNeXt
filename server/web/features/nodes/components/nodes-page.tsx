'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useEffect, useMemo, useState } from 'react';

import { EmptyState } from '@/components/feedback/empty-state';
import { ErrorState } from '@/components/feedback/error-state';
import { InlineMessage } from '@/components/feedback/inline-message';
import { LoadingState } from '@/components/feedback/loading-state';
import { PageHeader } from '@/components/layout/page-header';
import { AppModal } from '@/components/ui/app-modal';
import { StatusBadge } from '@/components/ui/status-badge';
import { getKernelCapability } from '@/features/capability/api/capability';
import { SourceImportPanel } from '@/features/import/components/source-import-page';
import {
  deleteProxyNode,
  deleteProxyNodes,
  getProxyNodeHealth,
  getProxyNodes,
  runProxyNodeHealthCheck,
  testProxyNodes,
  updateProxyNodeStatus,
} from '@/features/nodes/api/nodes';
import type { ProxyNodeItem } from '@/features/nodes/types';
import {
  DangerButton,
  PrimaryButton,
  ResourceInput,
  ResourceSelect,
  SecondaryButton,
} from '@/features/shared/components/resource-primitives';
import { formatDateTime, formatRelativeTime } from '@/lib/utils/date';

const proxyNodesQueryKey = ['proxy-nodes'] as const;
const nodeListLimit = 1000;

type FeedbackState = {
  tone: 'success' | 'danger' | 'info';
  message: string;
};

type HealthFilter = 'all' | 'success' | 'failed' | 'unknown' | 'quarantined';

function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试。';
}

function CompactMetric({
  label,
  value,
  hint,
}: {
  label: string;
  value: number | string;
  hint?: string;
}) {
  return (
    <div className="rounded-xl border border-[var(--border-default)] bg-[var(--surface-card)] px-4 py-3">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs font-medium text-[var(--foreground-secondary)]">
          {label}
        </span>
        <span className="text-xl font-semibold text-[var(--foreground-primary)] tabular-nums">
          {value}
        </span>
      </div>
      {hint ? (
        <p className="mt-1 truncate text-[11px] text-[var(--foreground-muted)]">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

function CompactPanel({ children }: { children: ReactNode }) {
  return (
    <section className="rounded-xl border border-[var(--border-default)] bg-[var(--surface-card)] shadow-[var(--shadow-soft)]">
      {children}
    </section>
  );
}

function nodeStatus(node: ProxyNodeItem) {
  if (!node.enabled) {
    return { label: '已禁用', variant: 'warning' as const };
  }
  if (node.quarantined_at) {
    return { label: '已隔离', variant: 'danger' as const };
  }
  switch (node.last_test_status) {
    case 'success':
      return { label: '可用', variant: 'success' as const };
    case 'failed':
      return { label: '失败', variant: 'danger' as const };
    default:
      return { label: '待检测', variant: 'info' as const };
  }
}

function healthQueryFilter(filter: HealthFilter) {
  if (filter === 'quarantined') {
    return { quarantined: true };
  }
  if (filter === 'all') {
    return {};
  }
  return {
    testStatus: filter,
    quarantined: filter === 'success' ? false : undefined,
  };
}

export function NodesPage() {
  const queryClient = useQueryClient();
  const [keywordInput, setKeywordInput] = useState('');
  const [keyword, setKeyword] = useState('');
  const [enabledFilter, setEnabledFilter] = useState<'all' | 'true' | 'false'>(
    'all',
  );
  const [healthFilter, setHealthFilter] = useState<HealthFilter>('all');
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [feedback, setFeedback] = useState<FeedbackState | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [isImportModalOpen, setIsImportModalOpen] = useState(false);

  const capabilityQuery = useQuery({
    queryKey: ['capability'],
    queryFn: getKernelCapability,
  });

  const nodesQuery = useQuery({
    queryKey: [
      ...proxyNodesQueryKey,
      'list',
      keyword,
      enabledFilter,
      healthFilter,
    ],
    queryFn: () =>
      getProxyNodes({
        limit: nodeListLimit,
        keyword,
        enabled: enabledFilter,
        ...healthQueryFilter(healthFilter),
      }),
    refetchInterval: autoRefresh ? 15000 : false,
  });

  const healthQuery = useQuery({
    queryKey: [...proxyNodesQueryKey, 'health'],
    queryFn: getProxyNodeHealth,
    refetchInterval: autoRefresh ? 15000 : false,
  });

  const invalidateNodeQueries = async () => {
    await queryClient.invalidateQueries({ queryKey: proxyNodesQueryKey });
  };

  const deleteMutation = useMutation({
    mutationFn: deleteProxyNode,
    onSuccess: async (_, id) => {
      setFeedback({ tone: 'success', message: '节点已删除。' });
      setSelectedIds((previous) => previous.filter((item) => item !== id));
      await invalidateNodeQueries();
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const batchDeleteMutation = useMutation({
    mutationFn: deleteProxyNodes,
    onSuccess: async (result) => {
      setFeedback({
        tone: 'success',
        message: `已删除 ${result.deleted} 个节点。`,
      });
      setSelectedIds([]);
      await invalidateNodeQueries();
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const testMutation = useMutation({
    mutationFn: (nodeIds: number[]) => testProxyNodes({ nodeIds }),
    onSuccess: async (result) => {
      const failed = result.filter((item) => item.status === 'failed').length;
      const quarantined = result.filter((item) => item.quarantined).length;
      setFeedback({
        tone: failed > 0 ? 'info' : 'success',
        message: `检测完成：共 ${result.length} 个，失败 ${failed} 个，当前隔离 ${quarantined} 个。`,
      });
      await invalidateNodeQueries();
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const healthRunMutation = useMutation({
    mutationFn: runProxyNodeHealthCheck,
    onSuccess: async (result) => {
      setFeedback({
        tone: result.failed > 0 ? 'info' : 'success',
        message: `全量探测完成：检测 ${result.checked}，成功 ${result.succeeded}，失败 ${result.failed}，隔离 ${result.quarantined}。`,
      });
      await invalidateNodeQueries();
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const statusMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      updateProxyNodeStatus(id, enabled),
    onSuccess: async (_, input) => {
      setFeedback({
        tone: 'success',
        message: input.enabled ? '节点已启用。' : '节点已禁用并移出运行配置。',
      });
      await invalidateNodeQueries();
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const nodes = useMemo(() => nodesQuery.data ?? [], [nodesQuery.data]);
  const selectedIdSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const visibleIds = useMemo(() => nodes.map((node) => node.id), [nodes]);
  const allVisibleSelected =
    visibleIds.length > 0 && visibleIds.every((id) => selectedIdSet.has(id));
  const summary = healthQuery.data;

  useEffect(() => {
    const visibleIdSet = new Set(visibleIds);
    setSelectedIds((previous) => previous.filter((id) => visibleIdSet.has(id)));
  }, [visibleIds]);

  const applyFilters = () => {
    setKeyword(keywordInput.trim());
    setSelectedIds([]);
    setFeedback(null);
  };

  const resetFilters = () => {
    setKeywordInput('');
    setKeyword('');
    setEnabledFilter('all');
    setHealthFilter('all');
    setSelectedIds([]);
    setFeedback(null);
  };

  const toggleVisibleSelection = () => {
    if (allVisibleSelected) {
      setSelectedIds((previous) =>
        previous.filter((id) => !visibleIds.includes(id)),
      );
      return;
    }
    setSelectedIds((previous) =>
      Array.from(new Set([...previous, ...visibleIds])),
    );
  };

  return (
    <div className="space-y-4">
      <PageHeader
        title="节点池"
        description="集中查看节点可用性、延迟与隔离状态；后台探测会自动把连续失败节点移出 Mihomo 运行配置。"
        action={
          <div className="flex flex-wrap gap-2">
            <SecondaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={() => setAutoRefresh((value) => !value)}
            >
              {autoRefresh ? '停止刷新' : '自动刷新'}
            </SecondaryButton>
            <SecondaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={() => healthRunMutation.mutate()}
              disabled={healthRunMutation.isPending}
            >
              {healthRunMutation.isPending
                ? '全量探测中...'
                : '探测全部启用节点'}
            </SecondaryButton>
            <PrimaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={() => setIsImportModalOpen(true)}
            >
              导入节点
            </PrimaryButton>
          </div>
        }
      />

      {feedback ? (
        <InlineMessage tone={feedback.tone} message={feedback.message} />
      ) : null}
      {capabilityQuery.data && !capabilityQuery.data.binary_exists ? (
        <InlineMessage tone="danger" message={capabilityQuery.data.message} />
      ) : null}

      <div className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-6">
        <CompactMetric label="节点总数" value={summary?.total ?? '—'} />
        <CompactMetric label="已启用" value={summary?.enabled ?? '—'} />
        <CompactMetric label="当前可用" value={summary?.healthy ?? '—'} />
        <CompactMetric label="最近失败" value={summary?.failed ?? '—'} />
        <CompactMetric label="已隔离" value={summary?.quarantined ?? '—'} />
        <CompactMetric
          label="最近探测"
          value={
            summary?.last_tested_at
              ? formatRelativeTime(summary.last_tested_at)
              : '未执行'
          }
          hint={
            summary?.last_tested_at
              ? formatDateTime(summary.last_tested_at)
              : undefined
          }
        />
      </div>

      <CompactPanel>
        <div className="grid gap-2 p-3 lg:grid-cols-[minmax(16rem,1fr)_11rem_11rem_auto]">
          <ResourceInput
            value={keywordInput}
            onChange={(event) => setKeywordInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                applyFilters();
              }
            }}
            className="!rounded-xl !px-3 !py-2"
            placeholder="搜索名称、协议或服务器地址"
            aria-label="节点关键字"
          />
          <ResourceSelect
            value={enabledFilter}
            onChange={(event) =>
              setEnabledFilter(event.target.value as 'all' | 'true' | 'false')
            }
            className="!rounded-xl !px-3 !py-2"
            aria-label="启用状态"
          >
            <option value="all">全部启用状态</option>
            <option value="true">仅启用</option>
            <option value="false">仅禁用</option>
          </ResourceSelect>
          <ResourceSelect
            value={healthFilter}
            onChange={(event) =>
              setHealthFilter(event.target.value as HealthFilter)
            }
            className="!rounded-xl !px-3 !py-2"
            aria-label="健康状态"
          >
            <option value="all">全部健康状态</option>
            <option value="success">可用</option>
            <option value="failed">最近失败</option>
            <option value="unknown">待检测</option>
            <option value="quarantined">已隔离</option>
          </ResourceSelect>
          <div className="flex gap-2">
            <PrimaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={applyFilters}
            >
              筛选
            </PrimaryButton>
            <SecondaryButton
              type="button"
              className="!rounded-xl !px-3 !py-2"
              onClick={resetFilters}
            >
              重置
            </SecondaryButton>
          </div>
        </div>
      </CompactPanel>

      <CompactPanel>
        <div className="flex flex-col gap-2 border-b border-[var(--border-default)] px-3 py-2.5 md:flex-row md:items-center md:justify-between">
          <div className="flex flex-wrap items-center gap-3 text-xs text-[var(--foreground-secondary)]">
            <span>显示 {nodes.length} 个节点</span>
            <span>已选择 {selectedIds.length} 个</span>
            {summary && summary.total > nodeListLimit ? (
              <span className="text-[var(--status-warning-foreground)]">
                当前筛选最多显示前 {nodeListLimit} 个
              </span>
            ) : null}
          </div>
          <div className="flex flex-wrap gap-2">
            <SecondaryButton
              type="button"
              className="!rounded-lg !px-2.5 !py-1.5 !text-xs"
              onClick={toggleVisibleSelection}
              disabled={nodes.length === 0}
            >
              {allVisibleSelected ? '取消当前全选' : '全选当前结果'}
            </SecondaryButton>
            <SecondaryButton
              type="button"
              className="!rounded-lg !px-2.5 !py-1.5 !text-xs"
              onClick={() => testMutation.mutate(selectedIds)}
              disabled={selectedIds.length === 0 || testMutation.isPending}
            >
              {testMutation.isPending ? '检测中...' : '检测所选'}
            </SecondaryButton>
            <DangerButton
              type="button"
              className="!rounded-lg !px-2.5 !py-1.5 !text-xs"
              onClick={() => {
                if (
                  selectedIds.length > 0 &&
                  window.confirm(
                    `确认删除所选 ${selectedIds.length} 个节点吗？`,
                  )
                ) {
                  batchDeleteMutation.mutate(selectedIds);
                }
              }}
              disabled={
                selectedIds.length === 0 || batchDeleteMutation.isPending
              }
            >
              {batchDeleteMutation.isPending ? '删除中...' : '删除所选'}
            </DangerButton>
          </div>
        </div>

        {nodesQuery.isLoading ? (
          <div className="p-4">
            <LoadingState />
          </div>
        ) : null}
        {nodesQuery.isError ? (
          <div className="p-4">
            <ErrorState
              title="加载节点失败"
              description={getErrorMessage(nodesQuery.error)}
            />
          </div>
        ) : null}
        {!nodesQuery.isLoading && !nodesQuery.isError && nodes.length === 0 ? (
          <div className="p-4">
            <EmptyState
              title="没有匹配节点"
              description="调整筛选条件，或导入 Clash/Mihomo 配置与订阅。"
            />
          </div>
        ) : null}

        {!nodesQuery.isLoading && !nodesQuery.isError && nodes.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[1040px] border-collapse text-left text-xs">
              <thead className="bg-[var(--surface-muted)] text-[var(--foreground-secondary)]">
                <tr>
                  <th className="w-10 px-3 py-2 font-medium">
                    <input
                      type="checkbox"
                      checked={allVisibleSelected}
                      onChange={toggleVisibleSelection}
                      aria-label="全选当前节点"
                      className="h-4 w-4 accent-[var(--brand-primary)]"
                    />
                  </th>
                  <th className="w-24 px-2 py-2 font-medium">状态</th>
                  <th className="min-w-56 px-2 py-2 font-medium">节点</th>
                  <th className="min-w-56 px-2 py-2 font-medium">出口地址</th>
                  <th className="min-w-40 px-2 py-2 font-medium">
                    来源 / 标签
                  </th>
                  <th className="w-28 px-2 py-2 font-medium">延迟</th>
                  <th className="w-36 px-2 py-2 font-medium">最近探测</th>
                  <th className="w-56 px-3 py-2 text-right font-medium">
                    操作
                  </th>
                </tr>
              </thead>
              <tbody>
                {nodes.map((node) => {
                  const status = nodeStatus(node);
                  return (
                    <tr
                      key={node.id}
                      className="border-t border-[var(--border-default)] transition hover:bg-[var(--surface-muted)]/70"
                    >
                      <td className="px-3 py-2.5 align-middle">
                        <input
                          type="checkbox"
                          checked={selectedIdSet.has(node.id)}
                          onChange={(event) =>
                            setSelectedIds((previous) =>
                              event.target.checked
                                ? Array.from(new Set([...previous, node.id]))
                                : previous.filter((id) => id !== node.id),
                            )
                          }
                          aria-label={`选择节点 ${node.name}`}
                          className="h-4 w-4 accent-[var(--brand-primary)]"
                        />
                      </td>
                      <td className="px-2 py-2.5 align-middle">
                        <StatusBadge
                          label={status.label}
                          variant={status.variant}
                          className="!px-2 !py-0.5"
                        />
                      </td>
                      <td className="px-2 py-2.5 align-middle">
                        <p
                          className="max-w-64 truncate font-medium text-[var(--foreground-primary)]"
                          title={node.name}
                        >
                          {node.name}
                        </p>
                        <p className="mt-0.5 text-[11px] text-[var(--foreground-muted)]">
                          #{node.id} · {node.type.toUpperCase()}
                          {node.consecutive_failures > 0
                            ? ` · 连续失败 ${node.consecutive_failures}`
                            : ''}
                        </p>
                      </td>
                      <td className="px-2 py-2.5 align-middle font-mono text-[11px] text-[var(--foreground-secondary)]">
                        <span
                          className="block max-w-64 truncate"
                          title={`${node.server}:${node.port}`}
                        >
                          {node.server}:{node.port}
                        </span>
                        {node.last_test_error ? (
                          <span
                            className="mt-0.5 block max-w-64 truncate text-[var(--status-danger-foreground)]"
                            title={node.last_test_error}
                          >
                            {node.last_test_error}
                          </span>
                        ) : null}
                      </td>
                      <td className="px-2 py-2.5 align-middle text-[11px] text-[var(--foreground-secondary)]">
                        <p
                          className="max-w-48 truncate"
                          title={node.source_config_name}
                        >
                          {node.source_config_name}
                        </p>
                        <p
                          className="mt-0.5 max-w-48 truncate text-[var(--foreground-muted)]"
                          title={node.tags || '未设置标签'}
                        >
                          {node.tags || '未设置标签'}
                        </p>
                      </td>
                      <td className="px-2 py-2.5 align-middle tabular-nums">
                        {node.last_latency_ms !== undefined ? (
                          <span className="font-medium text-[var(--foreground-primary)]">
                            {node.last_latency_ms} ms
                          </span>
                        ) : (
                          <span className="text-[var(--foreground-muted)]">
                            —
                          </span>
                        )}
                      </td>
                      <td
                        className="px-2 py-2.5 align-middle text-[11px] text-[var(--foreground-secondary)]"
                        title={
                          node.last_tested_at
                            ? formatDateTime(node.last_tested_at)
                            : '尚未探测'
                        }
                      >
                        {node.last_tested_at
                          ? formatRelativeTime(node.last_tested_at)
                          : '尚未探测'}
                      </td>
                      <td className="px-3 py-2.5 align-middle">
                        <div className="flex justify-end gap-1.5">
                          <SecondaryButton
                            type="button"
                            className="!rounded-lg !px-2.5 !py-1.5 !text-xs"
                            onClick={() => testMutation.mutate([node.id])}
                            disabled={testMutation.isPending}
                          >
                            探测
                          </SecondaryButton>
                          <SecondaryButton
                            type="button"
                            className="!rounded-lg !px-2.5 !py-1.5 !text-xs"
                            onClick={() =>
                              statusMutation.mutate({
                                id: node.id,
                                enabled: !node.enabled,
                              })
                            }
                            disabled={statusMutation.isPending}
                          >
                            {node.enabled ? '禁用' : '启用'}
                          </SecondaryButton>
                          <DangerButton
                            type="button"
                            className="!rounded-lg !px-2.5 !py-1.5 !text-xs"
                            onClick={() => {
                              if (
                                window.confirm(`确认删除节点“${node.name}”吗？`)
                              ) {
                                deleteMutation.mutate(node.id);
                              }
                            }}
                            disabled={deleteMutation.isPending}
                          >
                            删除
                          </DangerButton>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
      </CompactPanel>

      <AppModal
        isOpen={isImportModalOpen}
        onClose={() => setIsImportModalOpen(false)}
        title="导入节点"
        description="上传 Clash/Mihomo YAML，或填写返回 YAML 的订阅地址；解析和去重后直接写入节点池。"
        size="xl"
      >
        <SourceImportPanel
          embedded
          onImportSuccess={async (result) => {
            setFeedback({
              tone: 'success',
              message: `导入完成，新增 ${result.imported_nodes} 个节点，跳过 ${result.skipped_nodes} 个重复节点。`,
            });
            setSelectedIds([]);
            await invalidateNodeQueries();
            setIsImportModalOpen(false);
          }}
        />
      </AppModal>
    </div>
  );
}
