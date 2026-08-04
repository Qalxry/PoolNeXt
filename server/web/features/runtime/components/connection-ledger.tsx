'use client';

import { Fragment, useMemo, useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';

import { EmptyState } from '@/components/feedback/empty-state';
import { InlineMessage } from '@/components/feedback/inline-message';
import { AppCard } from '@/components/ui/app-card';
import { StatusBadge } from '@/components/ui/status-badge';
import type { RuntimeConnectionSnapshot } from '@/features/runtime/types';
import {
  ResourceInput,
  ResourceSelect,
} from '@/features/shared/components/resource-primitives';
import { formatDateTime } from '@/lib/utils/date';
import { formatBytes, formatBytesPerSecond } from '@/lib/utils/metrics';

function formatDuration(seconds: number) {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

export function ConnectionLedger({
  snapshot,
  running,
}: {
  snapshot?: RuntimeConnectionSnapshot;
  running: boolean;
}) {
  const [keyword, setKeyword] = useState('');
  const [state, setState] = useState<'all' | 'active' | 'closed'>('all');
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const items = useMemo(() => {
    const normalized = keyword.trim().toLowerCase();
    return (snapshot?.items ?? []).filter((item) => {
      if (state !== 'all' && item.state !== state) return false;
      if (!normalized) return true;
      return [
        item.source,
        item.destination,
        item.inbound_name,
        item.rule,
        item.rule_payload,
        item.policy_group,
        item.outbound_node,
        item.process,
      ]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(normalized));
    });
  }, [keyword, snapshot?.items, state]);

  return (
    <AppCard
      title="转发连接"
      description="来自 Mihomo Controller 的连接级元数据；结束记录仅在当前 PoolNeXt 进程内有界保留。"
      action={
        <span className="text-[10px] text-[var(--foreground-muted)]">
          {snapshot?.collected_at
            ? `采集 ${formatDateTime(snapshot.collected_at)}`
            : '尚未采集'}
        </span>
      }
    >
      <div className="metric-strip mb-2 grid-cols-2 md:grid-cols-5">
        <div className="flex min-h-10 items-center justify-between px-2.5">
          <span className="text-[10px] text-[var(--foreground-muted)]">
            活动
          </span>
          <strong className="text-sm tabular-nums">
            {snapshot?.active_count ?? 0}
          </strong>
        </div>
        <div className="flex min-h-10 items-center justify-between px-2.5">
          <span className="text-[10px] text-[var(--foreground-muted)]">
            上传
          </span>
          <strong className="text-xs tabular-nums">
            {formatBytesPerSecond(snapshot?.upload_rate)}
          </strong>
        </div>
        <div className="flex min-h-10 items-center justify-between px-2.5">
          <span className="text-[10px] text-[var(--foreground-muted)]">
            下载
          </span>
          <strong className="text-xs tabular-nums">
            {formatBytesPerSecond(snapshot?.download_rate)}
          </strong>
        </div>
        <div className="flex min-h-10 items-center justify-between px-2.5">
          <span className="text-[10px] text-[var(--foreground-muted)]">
            累计流量
          </span>
          <strong className="text-xs tabular-nums">
            {formatBytes(
              (snapshot?.upload_total ?? 0) + (snapshot?.download_total ?? 0),
            )}
          </strong>
        </div>
        <div className="flex min-h-10 items-center justify-between px-2.5">
          <span className="text-[10px] text-[var(--foreground-muted)]">
            内核内存
          </span>
          <strong className="text-xs tabular-nums">
            {formatBytes(snapshot?.memory)}
          </strong>
        </div>
      </div>

      {running && snapshot && !snapshot.available ? (
        <div className="mb-2">
          <InlineMessage
            tone="danger"
            message={`连接采集不可用：${snapshot.error || 'Controller 暂未响应'}`}
          />
        </div>
      ) : null}

      <div className="mb-2 flex flex-col gap-1.5 md:flex-row">
        <ResourceInput
          className="md:max-w-sm"
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          placeholder="搜索来源、目标、规则、策略组或节点"
        />
        <ResourceSelect
          className="md:w-32"
          value={state}
          onChange={(event) =>
            setState(event.target.value as 'all' | 'active' | 'closed')
          }
        >
          <option value="all">全部状态</option>
          <option value="active">活动</option>
          <option value="closed">已结束</option>
        </ResourceSelect>
      </div>

      {items.length === 0 ? (
        <EmptyState
          title={running ? '暂无匹配连接' : 'Mihomo 未运行'}
          description={
            running
              ? '建立代理连接后，这里会显示入口、规则、策略组、节点和流量。'
              : '启动 Mihomo 后才会采集结构化转发连接。'
          }
        />
      ) : (
        <div className="max-h-[560px] overflow-auto rounded-[var(--radius-control)] border border-[var(--border-default)]">
          <table className="w-full min-w-[1180px] border-collapse text-left text-[10px]">
            <thead className="sticky top-0 z-10 bg-[var(--surface-muted)] text-[var(--foreground-secondary)]">
              <tr>
                <th className="w-7 px-1.5 py-1.5 font-medium" />
                <th className="w-32 px-1.5 py-1.5 font-medium">时间</th>
                <th className="w-20 px-1.5 py-1.5 font-medium">状态</th>
                <th className="min-w-40 px-1.5 py-1.5 font-medium">
                  入口 / 来源
                </th>
                <th className="min-w-52 px-1.5 py-1.5 font-medium">目标</th>
                <th className="min-w-40 px-1.5 py-1.5 font-medium">规则</th>
                <th className="min-w-36 px-1.5 py-1.5 font-medium">策略组</th>
                <th className="min-w-36 px-1.5 py-1.5 font-medium">出口节点</th>
                <th className="w-24 px-1.5 py-1.5 text-right font-medium">
                  ↑ / ↓
                </th>
                <th className="w-16 px-1.5 py-1.5 text-right font-medium">
                  时长
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => {
                const isExpanded = expanded.has(item.id);
                return (
                  <Fragment key={item.id}>
                    <tr
                      className="cursor-pointer border-t border-[var(--border-default)] hover:bg-[var(--surface-muted)]/70"
                      onClick={() =>
                        setExpanded((current) => {
                          const next = new Set(current);
                          if (next.has(item.id)) next.delete(item.id);
                          else next.add(item.id);
                          return next;
                        })
                      }
                    >
                      <td className="h-8 px-1.5 py-0">
                        {isExpanded ? (
                          <ChevronDown className="h-3.5 w-3.5" />
                        ) : (
                          <ChevronRight className="h-3.5 w-3.5" />
                        )}
                      </td>
                      <td className="h-8 px-1.5 py-0 font-mono text-[9px] text-[var(--foreground-muted)]">
                        {formatDateTime(item.started_at)}
                      </td>
                      <td className="h-8 px-1.5 py-0">
                        <div className="flex items-center gap-1">
                          <StatusBadge
                            label={item.state === 'active' ? '活动' : '结束'}
                            variant={
                              item.state === 'active' ? 'success' : 'info'
                            }
                          />
                          <span className="font-mono text-[9px]">
                            {item.network}
                          </span>
                        </div>
                      </td>
                      <td className="h-8 px-1.5 py-0">
                        <span
                          className="block max-w-44 truncate"
                          title={item.inbound_name}
                        >
                          {item.inbound_name || item.inbound_type || '—'}
                        </span>
                        <span className="block max-w-44 truncate font-mono text-[9px] text-[var(--foreground-muted)]">
                          {item.source || '—'}
                        </span>
                      </td>
                      <td className="h-8 px-1.5 py-0 font-mono">
                        <span
                          className="block max-w-64 truncate"
                          title={item.destination}
                        >
                          {item.destination || '—'}
                        </span>
                      </td>
                      <td className="h-8 px-1.5 py-0">
                        <span
                          className="block max-w-44 truncate"
                          title={`${item.rule} ${item.rule_payload}`}
                        >
                          {item.rule || '固定入口'}
                          {item.rule_payload ? ` · ${item.rule_payload}` : ''}
                        </span>
                      </td>
                      <td className="h-8 px-1.5 py-0">
                        <span
                          className="block max-w-40 truncate"
                          title={item.policy_group}
                        >
                          {item.policy_group || '—'}
                        </span>
                      </td>
                      <td className="h-8 px-1.5 py-0">
                        <span
                          className="block max-w-40 truncate"
                          title={item.outbound_node}
                        >
                          {item.outbound_node || '—'}
                        </span>
                      </td>
                      <td className="h-8 px-1.5 py-0 text-right font-mono text-[9px] tabular-nums">
                        {formatBytes(item.upload)} /{' '}
                        {formatBytes(item.download)}
                      </td>
                      <td className="h-8 px-1.5 py-0 text-right font-mono tabular-nums">
                        {formatDuration(item.duration_seconds)}
                      </td>
                    </tr>
                    {isExpanded ? (
                      <tr className="border-t border-[var(--border-default)] bg-[var(--surface-base)]">
                        <td colSpan={10} className="px-3 py-2">
                          <dl className="grid gap-x-4 gap-y-1 text-[10px] md:grid-cols-[7rem_minmax(0,1fr)_7rem_minmax(0,1fr)]">
                            <dt className="text-[var(--foreground-muted)]">
                              连接 ID
                            </dt>
                            <dd className="font-mono break-all">{item.id}</dd>
                            <dt className="text-[var(--foreground-muted)]">
                              出口链
                            </dt>
                            <dd>{item.chain.join(' → ') || '—'}</dd>
                            <dt className="text-[var(--foreground-muted)]">
                              目标 IP
                            </dt>
                            <dd className="font-mono">
                              {item.destination_ip || '—'}
                            </dd>
                            <dt className="text-[var(--foreground-muted)]">
                              入口用户
                            </dt>
                            <dd>{item.inbound_user || '—'}</dd>
                            <dt className="text-[var(--foreground-muted)]">
                              进程
                            </dt>
                            <dd>{item.process || '—'}</dd>
                            <dt className="text-[var(--foreground-muted)]">
                              进程路径
                            </dt>
                            <dd className="font-mono break-all">
                              {item.process_path || '—'}
                            </dd>
                            <dt className="text-[var(--foreground-muted)]">
                              结束时间
                            </dt>
                            <dd>
                              {item.ended_at
                                ? formatDateTime(item.ended_at)
                                : '活动中'}
                            </dd>
                            <dt className="text-[var(--foreground-muted)]">
                              Provider 链
                            </dt>
                            <dd>{item.provider_chain?.join(' → ') || '—'}</dd>
                          </dl>
                        </td>
                      </tr>
                    ) : null}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </AppCard>
  );
}
