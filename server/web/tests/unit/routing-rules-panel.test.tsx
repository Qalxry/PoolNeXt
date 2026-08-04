import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type {
  PortProfileWithNodes,
  RoutingRulePayload,
} from '@/features/workspace/types';

let queryRules: RoutingRulePayload[] = [];

vi.mock('@tanstack/react-query', () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useQuery: () => ({ data: queryRules, isLoading: false }),
}));

vi.mock('@/features/workspace/api/workspace', () => ({
  getRoutingRules: vi.fn(),
  replaceRoutingRules: vi.fn(),
}));

import { RoutingRulesPanel } from '@/features/workspace/components/routing-rules-panel';

const profiles = [
  {
    profile: {
      id: 7,
      name: '主策略组',
      include_in_runtime: true,
    },
  },
] as PortProfileWithNodes[];

function rule(
  id: number,
  name: string,
  ruleType: RoutingRulePayload['rule_type'],
): RoutingRulePayload {
  return {
    id,
    name,
    rule_type: ruleType,
    payload: ruleType === 'MATCH' ? '' : 'example.com',
    target_profile_id: 7,
    enabled: true,
  };
}

describe('RoutingRulesPanel', () => {
  beforeEach(() => {
    queryRules = [];
  });

  it('inserts new rules before the final default rule', async () => {
    queryRules = [
      rule(1, '域名规则', 'DOMAIN-SUFFIX'),
      rule(2, '默认出口', 'MATCH'),
    ];
    const user = userEvent.setup();
    render(<RoutingRulesPanel profiles={profiles} />);

    await user.click(screen.getByRole('button', { name: '添加规则' }));

    const rows = screen.getAllByRole('row').slice(1);
    expect(rows).toHaveLength(3);
    expect(within(rows[2]).getByDisplayValue('默认出口')).toBeInTheDocument();
    expect(within(rows[2]).getByTitle('上移')).toBeDisabled();
    expect(within(rows[1]).getByTitle('下移')).toBeDisabled();
  });

  it('moves a rule to the end when it becomes the default rule', async () => {
    queryRules = [
      rule(1, '第一条', 'DOMAIN'),
      rule(2, '第二条', 'DOMAIN-SUFFIX'),
    ];
    const user = userEvent.setup();
    render(<RoutingRulesPanel profiles={profiles} />);

    const firstRow = screen.getAllByRole('row')[1];
    await user.selectOptions(
      within(firstRow).getAllByRole('combobox')[0],
      'MATCH',
    );

    const rows = screen.getAllByRole('row').slice(1);
    expect(within(rows[0]).getByDisplayValue('第二条')).toBeInTheDocument();
    expect(within(rows[1]).getByDisplayValue('第一条')).toBeInTheDocument();
    expect(within(rows[1]).getAllByRole('combobox')[0]).toHaveValue('MATCH');
  });
});
