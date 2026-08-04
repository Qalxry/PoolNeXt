export interface KernelInstanceItem {
  id: number;
  kernel_type: string;
  status: string;
  pid?: number;
  work_dir: string;
  config_path: string;
  controller_address: string;
  active_config_checksum: string;
  active_profile_count: number;
  active_listener_count: number;
  last_action: string;
  last_error: string;
  last_started_at?: string;
  last_stopped_at?: string;
  last_reloaded_at?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeListenerItem {
  profile_id: number;
  profile_name: string;
  name: string;
  type: string;
  listen: string;
  port: number;
  proxy_group_name: string;
  entry_mode: 'fixed' | 'rule';
}

export interface RuntimeStatus {
  instance: KernelInstanceItem;
  running: boolean;
  auto_start_enabled: boolean;
  api_healthy: boolean;
  api_version?: string;
  profile_count: number;
  listener_count: number;
  listeners: RuntimeListenerItem[];
  config_pending: boolean;
  config_valid: boolean;
  config_error?: string;
  rendered_config_preview?: string;
}

export interface RuntimeLogItem {
  seq: number;
  stream: string;
  level: string;
  message: string;
  created_at: string;
}

export interface RuntimeLogList {
  items: RuntimeLogItem[];
}

export interface RuntimeConnectionItem {
  id: string;
  state: 'active' | 'closed';
  network: string;
  inbound_type: string;
  inbound_name: string;
  inbound_user?: string;
  source: string;
  destination: string;
  destination_ip?: string;
  process?: string;
  process_path?: string;
  rule?: string;
  rule_payload?: string;
  policy_group?: string;
  outbound_node?: string;
  chain: string[];
  provider_chain?: string[];
  upload: number;
  download: number;
  started_at: string;
  ended_at?: string;
  duration_seconds: number;
}

export interface RuntimeConnectionSnapshot {
  available: boolean;
  error?: string;
  collected_at: string;
  upload_rate: number;
  download_rate: number;
  upload_total: number;
  download_total: number;
  memory: number;
  active_count: number;
  items: RuntimeConnectionItem[];
}
