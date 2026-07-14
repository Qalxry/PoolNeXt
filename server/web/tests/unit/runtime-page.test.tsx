import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const { invalidateQueriesMock } = vi.hoisted(() => ({
  invalidateQueriesMock: vi.fn(),
}));

const runtimeStatus = {
  instance: {
    id: 1,
    kernel_type: 'mihomo',
    status: 'running',
    pid: 1234,
    work_dir: '/tmp/poolnext',
    config_path: '/tmp/poolnext/config.yaml',
    controller_address: '127.0.0.1:19090',
    active_config_checksum: 'checksum-123',
    active_profile_count: 1,
    active_listener_count: 1,
    last_action: 'start',
    last_error: '',
    created_at: '2026-07-14T00:00:00Z',
    updated_at: '2026-07-14T00:00:00Z',
  },
  running: true,
  api_healthy: true,
  api_version: 'v1.19.24',
  profile_count: 1,
  listener_count: 1,
  listeners: [],
  rendered_config_preview: [
    'secret: controller-secret-value',
    'proxies:',
    '  - password: node-password-value',
  ].join('\n'),
};
const runtimeLogs = { items: [] };

vi.mock('@tanstack/react-query', () => ({
  useQueryClient: () => ({ invalidateQueries: invalidateQueriesMock }),
  useMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === 'capability') {
      return {
        data: {
          binary_exists: true,
          supports_start: true,
          supports_reload: true,
          message: 'Mihomo 可用',
        },
      };
    }
    if (queryKey[0] === 'runtime' && queryKey[1] === 'status') {
      return { data: runtimeStatus, isLoading: false, isError: false };
    }
    return { data: runtimeLogs, isLoading: false };
  },
}));

vi.mock('@/features/runtime/api/runtime', () => ({
  getRuntimeLogs: vi.fn().mockResolvedValue({ items: [] }),
  getRuntimeStatus: vi.fn(),
  reloadRuntime: vi.fn(),
  startRuntime: vi.fn(),
  stopRuntime: vi.fn(),
}));

vi.mock('@/features/capability/api/capability', () => ({
  getKernelCapability: vi.fn(),
}));

import { RuntimePage } from '@/features/runtime/components/runtime-page';

describe('RuntimePage', () => {
  afterEach(() => {
    invalidateQueriesMock.mockReset();
  });

  it('shows only non-sensitive config metadata instead of rendered YAML', () => {
    const view = render(<RuntimePage />);

    expect(screen.getByText('最终配置摘要')).toBeInTheDocument();
    expect(screen.getByText('checksum-123')).toBeInTheDocument();
    expect(
      screen.queryByText(/controller-secret-value/),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/node-password-value/)).not.toBeInTheDocument();

    view.unmount();
  });
});
