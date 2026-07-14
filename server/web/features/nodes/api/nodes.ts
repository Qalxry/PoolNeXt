import { apiRequest } from '@/lib/api/client';

import type {
  NodeHealthRunResult,
  NodeHealthSummary,
  NodeTestExecution,
  ProxyNodeItem,
} from '@/features/nodes/types';

export function getProxyNodes(params: {
  limit?: number;
  keyword?: string;
  enabled?: string;
  testStatus?: string;
  quarantined?: boolean;
}) {
  const searchParams = new URLSearchParams();
  searchParams.set('limit', String(params.limit ?? 1000));
  if (params.keyword?.trim()) {
    searchParams.set('keyword', params.keyword.trim());
  }
  if (params.enabled && params.enabled !== 'all') {
    searchParams.set('enabled', params.enabled);
  }
  if (params.testStatus && params.testStatus !== 'all') {
    searchParams.set('test_status', params.testStatus);
  }
  if (params.quarantined !== undefined) {
    searchParams.set('quarantined', String(params.quarantined));
  }

  return apiRequest<ProxyNodeItem[]>(`/proxy-nodes?${searchParams.toString()}`);
}

export function getProxyNodeHealth() {
  return apiRequest<NodeHealthSummary>('/proxy-nodes/health');
}

export function runProxyNodeHealthCheck() {
  return apiRequest<NodeHealthRunResult>('/proxy-nodes/health/run', {
    method: 'POST',
  });
}

export function updateProxyNodeStatus(id: number, enabled: boolean) {
  return apiRequest<void>(`/proxy-nodes/${id}/status`, {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  });
}

export function deleteProxyNode(id: number) {
  return apiRequest<void>(`/proxy-nodes/${id}/delete`, {
    method: 'POST',
  });
}

export function deleteProxyNodes(nodeIds: number[]) {
  return apiRequest<{ deleted: number }>('/proxy-nodes/delete', {
    method: 'POST',
    body: JSON.stringify({ node_ids: nodeIds }),
  });
}

export function updateProxyNodeTags(nodeIds: number[], tags: string) {
  return apiRequest<{ updated: number }>('/proxy-nodes/tags', {
    method: 'POST',
    body: JSON.stringify({ node_ids: nodeIds, tags }),
  });
}

export function testProxyNodes(input: { nodeIds: number[] }) {
  return apiRequest<NodeTestExecution[]>('/proxy-nodes/test', {
    method: 'POST',
    body: JSON.stringify({
      node_ids: input.nodeIds,
    }),
  });
}
