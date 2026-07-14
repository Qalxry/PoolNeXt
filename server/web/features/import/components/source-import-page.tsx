'use client';

import { useMutation } from '@tanstack/react-query';
import { useMemo, useState } from 'react';

import { ErrorState } from '@/components/feedback/error-state';
import { InlineMessage } from '@/components/feedback/inline-message';
import { PageHeader } from '@/components/layout/page-header';
import { AppCard } from '@/components/ui/app-card';
import { StatusBadge } from '@/components/ui/status-badge';
import {
  importSourceConfig,
  parseSourceConfig,
  parseSourceConfigByURL,
  testParsedNodes,
} from '@/features/import/api/source-import';
import type {
  ParsedNodePreview,
  ParsedNodeTestResult,
  SourceImportResult,
  SourceParseResult,
} from '@/features/import/types';
import {
  PrimaryButton,
  ResourceField,
  ResourceInput,
  SecondaryButton,
} from '@/features/shared/components/resource-primitives';

type FeedbackState = {
  tone: 'success' | 'danger' | 'info';
  message: string;
};

type SourceInputMode = 'upload' | 'subscription_url';

interface SourceImportPanelProps {
  embedded?: boolean;
  onImportSuccess?: (result: SourceImportResult) => void;
}

function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试。';
}

function isValidSubscriptionURL(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

function maskSubscriptionURL(value?: string) {
  if (!value) {
    return '订阅地址已隐藏';
  }

  try {
    const url = new URL(value);
    const hasSearch = url.search.length > 0;
    const hasHash = url.hash.length > 0;
    url.username = '';
    url.password = '';
    url.search = hasSearch ? '?***' : '';
    url.hash = hasHash ? '#***' : '';
    return url.toString();
  } catch {
    return '订阅地址已隐藏';
  }
}

function sourceConfigOriginLabel(
  sourceConfig: SourceParseResult['source_config'],
) {
  if (sourceConfig.source_type === 'subscription_url') {
    return `来源订阅 ${maskSubscriptionURL(sourceConfig.source_url)}`;
  }

  return `文件 ${sourceConfig.filename}`;
}

function duplicateScopeLabel(scope: string) {
  switch (scope) {
    case 'batch':
      return '批内重复';
    case 'database':
      return '库内已存在';
    default:
      return '可导入';
  }
}

function duplicateScopeVariant(scope: string) {
  return scope === 'none' ? ('success' as const) : ('warning' as const);
}

export function SourceImportPanel({
  embedded = false,
  onImportSuccess,
}: SourceImportPanelProps) {
  const [sourceMode, setSourceMode] = useState<SourceInputMode>('upload');
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [subscriptionURL, setSubscriptionURL] = useState('');
  const [parseResult, setParseResult] = useState<SourceParseResult | null>(
    null,
  );
  const [previewNodes, setPreviewNodes] = useState<ParsedNodePreview[]>([]);
  const [selectedFingerprints, setSelectedFingerprints] = useState<string[]>(
    [],
  );
  const [testResults, setTestResults] = useState<ParsedNodeTestResult[]>([]);
  const [feedback, setFeedback] = useState<FeedbackState | null>(null);

  const importableNodes = useMemo(
    () => previewNodes.filter((node) => node.duplicate_scope === 'none'),
    [previewNodes],
  );
  const selectedSet = useMemo(
    () => new Set(selectedFingerprints),
    [selectedFingerprints],
  );

  const parseMutation = useMutation({
    mutationFn: async () => {
      if (sourceMode === 'upload') {
        if (!selectedFile) {
          throw new Error('请先选择 YAML 文件。');
        }
        return parseSourceConfig(selectedFile);
      }

      const normalizedURL = subscriptionURL.trim();
      if (!isValidSubscriptionURL(normalizedURL)) {
        throw new Error('请输入有效的 http/https 订阅地址。');
      }

      return parseSourceConfigByURL(normalizedURL);
    },
    onSuccess: (result) => {
      setParseResult(result);
      setPreviewNodes(result.nodes);
      setSelectedFingerprints(
        result.nodes
          .filter((node) => node.duplicate_scope === 'none')
          .map((node) => node.fingerprint),
      );
      setTestResults([]);
      setFeedback({
        tone: 'success',
        message: `解析完成，识别到 ${result.summary.valid_nodes} 个有效节点。`,
      });
    },
    onError: (error) => {
      setParseResult(null);
      setPreviewNodes([]);
      setSelectedFingerprints([]);
      setTestResults([]);
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const importMutation = useMutation({
    mutationFn: async () => {
      if (!parseResult) {
        throw new Error('请先完成解析。');
      }
      return importSourceConfig(
        parseResult.source_config.id,
        selectedFingerprints,
      );
    },
    onSuccess: (result) => {
      onImportSuccess?.(result);
      setFeedback({
        tone: 'success',
        message: `导入完成，新增 ${result.imported_nodes} 个节点，跳过 ${result.skipped_nodes} 个重复节点。`,
      });
      setParseResult((previous) =>
        previous
          ? {
              ...previous,
              source_config: {
                ...previous.source_config,
                status: 'imported',
                imported_nodes: result.imported_nodes,
                duplicate_nodes: result.skipped_nodes,
              },
            }
          : previous,
      );
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const testMutation = useMutation({
    mutationFn: async () => {
      if (!parseResult) {
        throw new Error('请先完成解析。');
      }
      if (selectedFingerprints.length === 0) {
        throw new Error('请先选择要测速的节点。');
      }
      return testParsedNodes({
        sourceConfigId: parseResult.source_config.id,
        fingerprints: selectedFingerprints,
      });
    },
    onSuccess: (result) => {
      setTestResults(result);
      setFeedback({
        tone: 'success',
        message: `测速完成，共返回 ${result.length} 条结果。`,
      });
    },
    onError: (error) => {
      setFeedback({ tone: 'danger', message: getErrorMessage(error) });
    },
  });

  const summary = useMemo(() => {
    if (!parseResult) {
      return null;
    }

    return {
      total_nodes: previewNodes.length + parseResult.errors.length,
      valid_nodes: previewNodes.length,
      invalid_nodes: parseResult.errors.length,
      duplicate_nodes: previewNodes.filter(
        (node) => node.duplicate_scope !== 'none',
      ).length,
      importable_nodes: importableNodes.length,
    };
  }, [importableNodes.length, parseResult, previewNodes]);

  const importSourceDescription = useMemo(() => {
    if (!parseResult) {
      return '';
    }

    return `当前导入记录 #${parseResult.source_config.id}，${sourceConfigOriginLabel(parseResult.source_config)}。当前已选择 ${selectedFingerprints.length} 个节点。`;
  }, [parseResult, selectedFingerprints.length]);

  const handleToggleNode = (fingerprint: string, checked: boolean) => {
    setSelectedFingerprints((previous) =>
      checked
        ? Array.from(new Set([...previous, fingerprint]))
        : previous.filter((item) => item !== fingerprint),
    );
  };

  return (
    <div className="space-y-3">
      {!embedded ? <PageHeader title="配置导入" /> : null}

      {feedback ? (
        <InlineMessage tone={feedback.tone} message={feedback.message} />
      ) : null}

      <AppCard title="上传并解析">
        <div className="space-y-2.5">
          <div className="inline-flex rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--surface-base)] p-0.5">
            <PrimaryButton
              type="button"
              aria-pressed={sourceMode === 'upload'}
              onClick={() => setSourceMode('upload')}
              className={
                sourceMode === 'upload'
                  ? '!h-7'
                  : '!h-7 bg-transparent text-[var(--foreground-secondary)] hover:bg-[var(--control-background-hover)]'
              }
            >
              上传 YAML
            </PrimaryButton>
            <PrimaryButton
              type="button"
              aria-pressed={sourceMode === 'subscription_url'}
              onClick={() => setSourceMode('subscription_url')}
              className={
                sourceMode === 'subscription_url'
                  ? '!h-7'
                  : '!h-7 bg-transparent text-[var(--foreground-secondary)] hover:bg-[var(--control-background-hover)]'
              }
            >
              订阅地址
            </PrimaryButton>
          </div>

          {sourceMode === 'upload' ? (
            <ResourceField label="YAML 文件">
              <input
                type="file"
                accept=".yaml,.yml,text/yaml,text/x-yaml,application/x-yaml"
                onChange={(event) =>
                  setSelectedFile(event.target.files?.[0] ?? null)
                }
                className="block h-9 w-full rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] px-2.5 py-1.5 text-xs text-[var(--foreground-primary)]"
              />
            </ResourceField>
          ) : (
            <ResourceField
              label="订阅地址"
              hint="仅支持返回 Clash/Mihomo YAML 的 http/https 地址。"
            >
              <ResourceInput
                type="url"
                aria-label="订阅地址"
                value={subscriptionURL}
                onChange={(event) => setSubscriptionURL(event.target.value)}
                placeholder="https://example.com/subscription.yaml"
              />
            </ResourceField>
          )}

          <div className="flex flex-wrap gap-1.5">
            <PrimaryButton
              type="button"
              onClick={() => {
                setFeedback(null);
                parseMutation.mutate();
              }}
              disabled={parseMutation.isPending}
            >
              {parseMutation.isPending ? '解析中...' : '开始解析'}
            </PrimaryButton>
            <SecondaryButton
              type="button"
              onClick={() => {
                setSourceMode('upload');
                setSelectedFile(null);
                setSubscriptionURL('');
                setParseResult(null);
                setPreviewNodes([]);
                setSelectedFingerprints([]);
                setTestResults([]);
                setFeedback(null);
              }}
            >
              清空
            </SecondaryButton>
          </div>
        </div>
      </AppCard>

      {parseMutation.isError ? (
        <ErrorState
          title="解析失败"
          description={getErrorMessage(parseMutation.error)}
        />
      ) : null}

      {parseResult && summary ? (
        <>
          <div className="metric-strip grid-cols-2 md:grid-cols-3 lg:grid-cols-5">
            <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
              <span className="text-[10px] text-[var(--foreground-secondary)]">
                总节点
              </span>
              <span className="text-sm font-semibold text-[var(--foreground-primary)] tabular-nums">
                {summary.total_nodes}
              </span>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
              <span className="text-[10px] text-[var(--foreground-secondary)]">
                有效
              </span>
              <span className="text-sm font-semibold text-[var(--foreground-primary)] tabular-nums">
                {summary.valid_nodes}
              </span>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
              <span className="text-[10px] text-[var(--foreground-secondary)]">
                错误
              </span>
              <span className="text-sm font-semibold text-[var(--status-danger-foreground)] tabular-nums">
                {summary.invalid_nodes}
              </span>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
              <span className="text-[10px] text-[var(--foreground-secondary)]">
                重复
              </span>
              <span className="text-sm font-semibold text-[var(--status-warning-foreground)] tabular-nums">
                {summary.duplicate_nodes}
              </span>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-2 px-3 py-1.5">
              <span className="text-[10px] text-[var(--foreground-secondary)]">
                可导入
              </span>
              <span className="text-sm font-semibold text-[var(--status-success-foreground)] tabular-nums">
                {summary.importable_nodes}
              </span>
            </div>
          </div>

          <AppCard title="导入确认" description={importSourceDescription}>
            <div className="flex flex-wrap gap-3">
              <PrimaryButton
                type="button"
                onClick={() => {
                  setFeedback(null);
                  testMutation.mutate();
                }}
                disabled={
                  testMutation.isPending || selectedFingerprints.length === 0
                }
              >
                {testMutation.isPending ? '测速中...' : '测速选中节点'}
              </PrimaryButton>
              <PrimaryButton
                type="button"
                onClick={() => {
                  setFeedback(null);
                  importMutation.mutate();
                }}
                disabled={
                  importMutation.isPending || selectedFingerprints.length === 0
                }
              >
                {importMutation.isPending ? '导入中...' : '导入节点池'}
              </PrimaryButton>
              <SecondaryButton
                type="button"
                onClick={() =>
                  setSelectedFingerprints(
                    previewNodes
                      .filter((node) => node.duplicate_scope === 'none')
                      .map((node) => node.fingerprint),
                  )
                }
              >
                选择全部可导入
              </SecondaryButton>
              <SecondaryButton
                type="button"
                onClick={() => setSelectedFingerprints([])}
              >
                清空选择
              </SecondaryButton>
              <SecondaryButton
                type="button"
                onClick={() => {
                  if (selectedFingerprints.length === 0) {
                    setFeedback({
                      tone: 'danger',
                      message: '请先选择要删除的预览节点。',
                    });
                    return;
                  }
                  setPreviewNodes((previous) =>
                    previous.filter(
                      (node) => !selectedSet.has(node.fingerprint),
                    ),
                  );
                  setSelectedFingerprints([]);
                  setTestResults([]);
                  setFeedback({
                    tone: 'info',
                    message:
                      '已从当前预览中移除选中节点，本次导入和测速都不会再包含它们。',
                  });
                }}
              >
                删除选中预览
              </SecondaryButton>
            </div>
          </AppCard>

          <div className="grid gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
            <AppCard
              title="节点预览"
              description="默认最多展示前 100 个解析结果，支持勾选后批量测速或从本次预览移除。"
            >
              <div className="max-h-[560px] overflow-auto rounded-lg border border-[var(--border-default)]">
                <table className="w-full min-w-[680px] border-collapse text-left text-xs">
                  <thead className="sticky top-0 z-10 bg-[var(--surface-muted)] text-[var(--foreground-secondary)]">
                    <tr>
                      <th className="w-10 px-3 py-2 font-medium">选</th>
                      <th className="min-w-64 px-2 py-2 font-medium">节点</th>
                      <th className="w-28 px-2 py-2 font-medium">协议</th>
                      <th className="min-w-56 px-2 py-2 font-medium">出口</th>
                      <th className="w-28 px-3 py-2 font-medium">导入状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {previewNodes.map((node, index) => (
                      <tr
                        key={`${node.fingerprint}-${index}`}
                        className="border-t border-[var(--border-default)] hover:bg-[var(--surface-muted)]/70"
                      >
                        <td className="px-3 py-2">
                          <input
                            type="checkbox"
                            checked={selectedSet.has(node.fingerprint)}
                            disabled={node.duplicate_scope !== 'none'}
                            onChange={(event) =>
                              handleToggleNode(
                                node.fingerprint,
                                event.target.checked,
                              )
                            }
                            aria-label={`选择预览节点 ${node.name}`}
                            className="h-4 w-4 accent-[var(--brand-primary)]"
                          />
                        </td>
                        <td className="px-2 py-2">
                          <p
                            className="max-w-80 truncate font-medium text-[var(--foreground-primary)]"
                            title={node.name}
                          >
                            {node.name}
                          </p>
                        </td>
                        <td className="px-2 py-2 text-[var(--foreground-secondary)] uppercase">
                          {node.type}
                        </td>
                        <td className="px-2 py-2 font-mono text-[11px] text-[var(--foreground-secondary)]">
                          {node.server}:{node.port}
                        </td>
                        <td className="px-3 py-2">
                          <StatusBadge
                            label={duplicateScopeLabel(node.duplicate_scope)}
                            variant={duplicateScopeVariant(
                              node.duplicate_scope,
                            )}
                            className="!px-2 !py-0.5"
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </AppCard>

            <AppCard
              title="解析错误"
              description="服务端会保留错误信息，便于后续补充更细的兼容规则。"
            >
              <div className="space-y-3">
                {parseResult.errors.length === 0 ? (
                  <p className="text-sm text-[var(--foreground-secondary)]">
                    当前没有解析错误。
                  </p>
                ) : (
                  parseResult.errors.map((issue, index) => (
                    <div
                      key={`${issue.index}-${index}`}
                      className="rounded-[var(--radius-control)] border border-[var(--status-danger-border)] bg-[var(--status-danger-soft)] p-3 text-sm text-[var(--status-danger-foreground)]"
                    >
                      <p className="font-medium">
                        条目 #{issue.index + 1}
                        {issue.name ? ` · ${issue.name}` : ''}
                      </p>
                      <p className="mt-1">{issue.message}</p>
                    </div>
                  ))
                )}
              </div>
            </AppCard>

            <AppCard
              title="测速结果"
              description="导入前测速结果仅用于当前预览，不会写入节点池最近状态。"
            >
              <div className="space-y-3">
                {testResults.length === 0 ? (
                  <p className="text-sm text-[var(--foreground-secondary)]">
                    选择节点后点击“测速选中节点”即可查看结果。
                  </p>
                ) : (
                  testResults.map((item, index) => (
                    <div
                      key={`${item.node_name}-${index}`}
                      className="rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--surface-muted)] p-3"
                    >
                      <p className="text-sm font-semibold text-[var(--foreground-primary)]">
                        {item.node_name}
                      </p>
                      <div className="mt-2 text-xs leading-6 text-[var(--foreground-secondary)]">
                        <p>
                          结果：{item.status === 'success' ? '成功' : '失败'}
                        </p>
                        <p>目标：{item.dial_address}</p>
                        <p>
                          耗时：
                          {item.latency_ms !== undefined
                            ? ` ${item.latency_ms} ms`
                            : ' 未记录'}
                        </p>
                        {item.error_message ? (
                          <p>错误：{item.error_message}</p>
                        ) : null}
                      </div>
                    </div>
                  ))
                )}
              </div>
            </AppCard>
          </div>
        </>
      ) : null}
    </div>
  );
}

export function SourceImportPage() {
  return <SourceImportPanel />;
}
