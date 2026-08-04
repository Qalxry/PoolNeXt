export type PortProfileStrategy =
  | 'select'
  | 'url-test'
  | 'fallback'
  | 'load-balance';

export type LoadBalanceStrategy =
  | 'consistent-hashing'
  | 'round-robin'
  | 'sticky-sessions';

export type PortProfileEntryMode = 'fixed' | 'rule';

export interface PortProfileProxySettings {
  strategy_type: PortProfileStrategy;
  entry_mode: PortProfileEntryMode;
  test_url: string;
  test_interval_seconds: number;
  test_timeout_milliseconds: number;
  max_failed_times: number;
  expected_status: string;
  url_test_tolerance_milliseconds: number;
  load_balance_strategy: LoadBalanceStrategy;
  load_balance_lazy: boolean;
  load_balance_disable_udp: boolean;
  udp_enabled: boolean;
  auth_enabled: boolean;
  auth_username: string;
  auth_password: string;
}

export interface RuntimeConfigItem {
  id: number;
  port_profile_id: number;
  kernel_type: string;
  checksum: string;
  rendered_config: string;
  created_at: string;
  updated_at: string;
}

export interface ProxyNodeOption {
  id: number;
  name: string;
  type: string;
  server: string;
  port: number;
  tags: string;
  source_config_name: string;
  enabled: boolean;
  last_test_status: string;
  last_latency_ms?: number;
  last_test_error?: string;
  last_tested_at?: string;
  consecutive_failures: number;
  quarantined_at?: string;
}

export interface PortProfileRecord {
  id: number;
  name: string;
  listen_host: string;
  mixed_port: number;
  socks_port: number;
  http_port: number;
  proxy_settings: PortProfileProxySettings;
  include_in_runtime: boolean;
  kernel_type: string;
  created_at: string;
  updated_at: string;
}

export interface PortProfileWithNodes {
  profile: PortProfileRecord;
  node_ids: number[];
  nodes: ProxyNodeOption[];
  runtime?: RuntimeConfigItem;
}

export interface PortProfilePayload {
  name: string;
  listen_host: string;
  mixed_port: number;
  socks_port: number;
  http_port: number;
  proxy_settings: PortProfileProxySettings;
  include_in_runtime: boolean;
  node_ids: number[];
}

export interface PortProfilePreview {
  profile: PortProfileRecord;
  node_ids: number[];
  nodes: ProxyNodeOption[];
  kernel_type: string;
  checksum: string;
  content: string;
}

export interface PortProfileTemplateRecord {
  id: number;
  name: string;
  listen_host: string;
  mixed_port: number;
  socks_port: number;
  http_port: number;
  proxy_settings: PortProfileProxySettings;
  include_in_runtime: boolean;
  created_at: string;
  updated_at: string;
}

export interface PortProfileTemplateItem {
  template: PortProfileTemplateRecord;
  node_ids: number[];
}

export type RoutingRuleType =
  | 'DOMAIN'
  | 'DOMAIN-SUFFIX'
  | 'DOMAIN-KEYWORD'
  | 'IP-CIDR'
  | 'SRC-IP-CIDR'
  | 'DST-PORT'
  | 'NETWORK'
  | 'IN-NAME'
  | 'MATCH';

export interface RoutingRuleItem {
  id: number;
  name: string;
  rule_type: RoutingRuleType;
  payload: string;
  target_profile_id: number;
  enabled: boolean;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export type RoutingRulePayload = Pick<
  RoutingRuleItem,
  'id' | 'name' | 'rule_type' | 'payload' | 'target_profile_id' | 'enabled'
>;
