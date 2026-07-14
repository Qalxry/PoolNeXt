export interface ProxyNodeItem {
  id: number;
  source_config_id: number;
  source_config_name: string;
  name: string;
  type: string;
  server: string;
  port: number;
  tags: string;
  enabled: boolean;
  last_test_status: string;
  last_latency_ms?: number;
  last_test_error?: string;
  last_tested_at?: string;
  consecutive_failures: number;
  consecutive_successes: number;
  quarantined_at?: string;
  created_at: string;
  updated_at: string;
}

export interface NodeTestExecution {
  node_id: number;
  node_name: string;
  status: string;
  latency_ms?: number;
  error_message?: string;
  test_url?: string;
  dial_address: string;
  cached: boolean;
  started_at: string;
  finished_at: string;
  last_tested_at?: string;
  consecutive_failures: number;
  consecutive_successes: number;
  quarantined: boolean;
  availability_changed: boolean;
}

export interface NodeHealthSummary {
  total: number;
  enabled: number;
  disabled: number;
  healthy: number;
  failed: number;
  unknown: number;
  quarantined: number;
  last_tested_at?: string;
}

export interface NodeHealthRunResult {
  checked: number;
  succeeded: number;
  failed: number;
  quarantined: number;
  results: NodeTestExecution[];
}
