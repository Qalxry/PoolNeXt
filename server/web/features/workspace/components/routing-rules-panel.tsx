'use client';

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowDown, ArrowUp, Plus, Save, Trash2 } from 'lucide-react';

import { EmptyState } from '@/components/feedback/empty-state';
import { InlineMessage } from '@/components/feedback/inline-message';
import { LoadingState } from '@/components/feedback/loading-state';
import { AppCard } from '@/components/ui/app-card';
import { Switch } from '@/components/ui/switch';
import {
  getRoutingRules,
  replaceRoutingRules,
} from '@/features/workspace/api/workspace';
import type {
  PortProfileWithNodes,
  RoutingRulePayload,
  RoutingRuleType,
} from '@/features/workspace/types';
import {
  PrimaryButton,
  ResourceInput,
  ResourceSelect,
  SecondaryButton,
} from '@/features/shared/components/resource-primitives';

const routingRuleQueryKey = ['workspace', 'routing-rules'] as const;

const ruleTypeOptions: Array<{
  value: RoutingRuleType;
  label: string;
  placeholder: string;
}> = [
  { value: 'DOMAIN', label: '域名', placeholder: 'api.example.com' },
  {
    value: 'DOMAIN-SUFFIX',
    label: '域名后缀',
    placeholder: 'example.com',
  },
  {
    value: 'DOMAIN-KEYWORD',
    label: '域名关键字',
    placeholder: 'google',
  },
  { value: 'IP-CIDR', label: '目标 CIDR', placeholder: '10.0.0.0/8' },
  {
    value: 'SRC-IP-CIDR',
    label: '来源 CIDR',
    placeholder: '192.168.1.0/24',
  },
  {
    value: 'DST-PORT',
    label: '目标端口',
    placeholder: '80/443/8000-9000',
  },
  { value: 'NETWORK', label: '网络', placeholder: 'TCP 或 UDP' },
  {
    value: 'IN-NAME',
    label: '入口名称',
    placeholder: '策略组-mixed-7890',
  },
  { value: 'MATCH', label: '默认规则', placeholder: '无需匹配内容' },
];

function newRule(profileId: number): RoutingRulePayload {
  return {
    id: 0,
    name: '',
    rule_type: 'DOMAIN-SUFFIX',
    payload: '',
    target_profile_id: profileId,
    enabled: true,
  };
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '规则保存失败。';
}

export function RoutingRulesPanel({
  profiles,
}: {
  profiles: PortProfileWithNodes[];
}) {
  const queryClient = useQueryClient();
  const [rules, setRules] = useState<RoutingRulePayload[]>([]);
  const [feedback, setFeedback] = useState<{
    tone: 'success' | 'danger' | 'info';
    message: string;
  } | null>(null);

  const rulesQuery = useQuery({
    queryKey: routingRuleQueryKey,
    queryFn: getRoutingRules,
  });
  useEffect(() => {
    if (!rulesQuery.data) return;
    setRules(
      rulesQuery.data.map((item) => ({
        id: item.id,
        name: item.name,
        rule_type: item.rule_type,
        payload: item.payload,
        target_profile_id: item.target_profile_id,
        enabled: item.enabled,
      })),
    );
  }, [rulesQuery.data]);

  const saveMutation = useMutation({
    mutationFn: () => replaceRoutingRules(rules),
    onSuccess: async (items) => {
      setRules(
        items.map((item) => ({
          id: item.id,
          name: item.name,
          rule_type: item.rule_type,
          payload: item.payload,
          target_profile_id: item.target_profile_id,
          enabled: item.enabled,
        })),
      );
      setFeedback({
        tone: 'success',
        message: '分流规则已保存；内核运行时请执行“应用并重载”。',
      });
      await queryClient.invalidateQueries({ queryKey: routingRuleQueryKey });
      await queryClient.invalidateQueries({ queryKey: ['runtime', 'status'] });
    },
    onError: (error) =>
      setFeedback({ tone: 'danger', message: errorMessage(error) }),
  });

  const enabledProfiles = profiles.filter(
    (item) => item.profile.include_in_runtime,
  );
  const defaultProfileId = enabledProfiles[0]?.profile.id ?? 0;
  const hasDefaultRule = rules.some((item) => item.rule_type === 'MATCH');

  const updateRule = (index: number, patch: Partial<RoutingRulePayload>) => {
    setRules((current) => {
      const result = current.map((item, itemIndex) =>
        itemIndex === index ? { ...item, ...patch } : item,
      );
      if (patch.rule_type !== 'MATCH') return result;

      const [defaultRule] = result.splice(index, 1);
      result.push(defaultRule);
      return result;
    });
  };

  const moveRule = (index: number, offset: -1 | 1) => {
    setRules((current) => {
      const nextIndex = index + offset;
      if (nextIndex < 0 || nextIndex >= current.length) return current;
      if (current[index].rule_type === 'MATCH') return current;
      if (offset === 1 && current[nextIndex].rule_type === 'MATCH') {
        return current;
      }
      const result = [...current];
      [result[index], result[nextIndex]] = [result[nextIndex], result[index]];
      return result;
    });
  };

  return (
    <AppCard
      title="规则分流"
      description="按顺序匹配参与规则分流的入口；固定入口不受这些规则影响。"
      action={
        <div className="flex gap-1.5">
          <SecondaryButton
            type="button"
            onClick={() =>
              setRules((current) => {
                const defaultIndex = current.findIndex(
                  (item) => item.rule_type === 'MATCH',
                );
                const insertAt =
                  defaultIndex === -1 ? current.length : defaultIndex;
                const result = [...current];
                result.splice(insertAt, 0, newRule(defaultProfileId));
                return result;
              })
            }
            disabled={defaultProfileId === 0}
          >
            <Plus className="h-3.5 w-3.5" aria-hidden="true" />
            添加规则
          </SecondaryButton>
          <PrimaryButton
            type="button"
            onClick={() => {
              setFeedback(null);
              saveMutation.mutate();
            }}
            disabled={saveMutation.isPending}
          >
            <Save className="h-3.5 w-3.5" aria-hidden="true" />
            {saveMutation.isPending ? '保存中...' : '保存规则'}
          </PrimaryButton>
        </div>
      }
    >
      {feedback ? (
        <div className="mb-2">
          <InlineMessage tone={feedback.tone} message={feedback.message} />
        </div>
      ) : null}
      {rulesQuery.isLoading ? <LoadingState /> : null}
      {!rulesQuery.isLoading && rules.length === 0 ? (
        <EmptyState
          title="暂无分流规则"
          description="现有固定入口会继续直接使用自己的策略组。需要规则入口时，至少添加一条匹配规则和一条末尾默认规则。"
        />
      ) : null}
      {rules.length > 0 ? (
        <div className="overflow-x-auto rounded-[var(--radius-control)] border border-[var(--border-default)]">
          <table className="w-full min-w-[980px] border-collapse text-left text-[11px]">
            <thead className="bg-[var(--surface-muted)] text-[10px] text-[var(--foreground-secondary)]">
              <tr>
                <th className="w-12 px-2 py-1.5 font-medium">启用</th>
                <th className="w-36 px-2 py-1.5 font-medium">名称</th>
                <th className="w-36 px-2 py-1.5 font-medium">匹配类型</th>
                <th className="min-w-60 px-2 py-1.5 font-medium">匹配内容</th>
                <th className="min-w-48 px-2 py-1.5 font-medium">目标策略组</th>
                <th className="w-28 px-2 py-1.5 text-right font-medium">
                  操作
                </th>
              </tr>
            </thead>
            <tbody>
              {rules.map((rule, index) => {
                const typeMeta =
                  ruleTypeOptions.find(
                    (item) => item.value === rule.rule_type,
                  ) ?? ruleTypeOptions[0];
                return (
                  <tr
                    key={`${rule.id || 'new'}-${index}`}
                    className="border-t border-[var(--border-default)]"
                  >
                    <td className="h-9 px-2 py-0">
                      <Switch
                        id={`routing-rule-${index}`}
                        checked={rule.enabled}
                        onCheckedChange={(enabled) =>
                          updateRule(index, { enabled })
                        }
                      />
                    </td>
                    <td className="h-9 px-1 py-1">
                      <ResourceInput
                        className="h-7 text-[11px]"
                        value={rule.name}
                        placeholder={`规则 ${index + 1}`}
                        onChange={(event) =>
                          updateRule(index, { name: event.target.value })
                        }
                      />
                    </td>
                    <td className="h-9 px-1 py-1">
                      <ResourceSelect
                        className="h-7 text-[11px]"
                        value={rule.rule_type}
                        onChange={(event) =>
                          updateRule(index, {
                            rule_type: event.target.value as RoutingRuleType,
                            payload:
                              event.target.value === 'MATCH'
                                ? ''
                                : rule.payload,
                          })
                        }
                      >
                        {ruleTypeOptions.map((item) => (
                          <option
                            key={item.value}
                            value={item.value}
                            disabled={
                              item.value === 'MATCH' &&
                              hasDefaultRule &&
                              rule.rule_type !== 'MATCH'
                            }
                          >
                            {item.label}
                          </option>
                        ))}
                      </ResourceSelect>
                    </td>
                    <td className="h-9 px-1 py-1">
                      <ResourceInput
                        className="h-7 font-mono text-[10px]"
                        value={rule.payload}
                        disabled={rule.rule_type === 'MATCH'}
                        placeholder={typeMeta.placeholder}
                        onChange={(event) =>
                          updateRule(index, { payload: event.target.value })
                        }
                      />
                    </td>
                    <td className="h-9 px-1 py-1">
                      <ResourceSelect
                        className="h-7 text-[11px]"
                        value={rule.target_profile_id}
                        onChange={(event) =>
                          updateRule(index, {
                            target_profile_id: Number(event.target.value),
                          })
                        }
                      >
                        <option value={0}>请选择</option>
                        {enabledProfiles.map((item) => (
                          <option key={item.profile.id} value={item.profile.id}>
                            {item.profile.name}
                          </option>
                        ))}
                      </ResourceSelect>
                    </td>
                    <td className="h-9 px-2 py-0">
                      <div className="flex justify-end gap-1">
                        <button
                          type="button"
                          title="上移"
                          onClick={() => moveRule(index, -1)}
                          disabled={index === 0 || rule.rule_type === 'MATCH'}
                          className="inline-flex h-6 w-6 items-center justify-center rounded-[4px] hover:bg-[var(--control-background-hover)] disabled:opacity-30"
                        >
                          <ArrowUp className="h-3.5 w-3.5" aria-hidden="true" />
                        </button>
                        <button
                          type="button"
                          title="下移"
                          onClick={() => moveRule(index, 1)}
                          disabled={
                            index === rules.length - 1 ||
                            rule.rule_type === 'MATCH' ||
                            rules[index + 1]?.rule_type === 'MATCH'
                          }
                          className="inline-flex h-6 w-6 items-center justify-center rounded-[4px] hover:bg-[var(--control-background-hover)] disabled:opacity-30"
                        >
                          <ArrowDown
                            className="h-3.5 w-3.5"
                            aria-hidden="true"
                          />
                        </button>
                        <button
                          type="button"
                          title="删除"
                          onClick={() =>
                            setRules((current) =>
                              current.filter(
                                (_, itemIndex) => itemIndex !== index,
                              ),
                            )
                          }
                          className="inline-flex h-6 w-6 items-center justify-center rounded-[4px] text-[var(--status-danger-foreground)] hover:bg-[var(--status-danger-soft)]"
                        >
                          <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
    </AppCard>
  );
}
