import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { authState, navigationState, replaceMock } = vi.hoisted(() => ({
  authState: {
    isAuthenticated: true,
    isLoading: false,
    user: {
      must_change_password: true,
    },
  },
  navigationState: {
    pathname: '/dashboard',
  },
  replaceMock: vi.fn(),
}));

vi.mock('next/navigation', () => ({
  usePathname: () => navigationState.pathname,
  useRouter: () => ({ replace: replaceMock }),
}));

vi.mock('@/components/providers/auth-provider', () => ({
  useAuth: () => authState,
}));

import { DashboardAuthGuard } from '@/features/auth/components/dashboard-auth-guard';

describe('DashboardAuthGuard', () => {
  beforeEach(() => {
    navigationState.pathname = '/dashboard';
    authState.isAuthenticated = true;
    authState.isLoading = false;
    authState.user.must_change_password = true;
    replaceMock.mockReset();
  });

  it('does not render dashboard children while redirecting a forced password change', async () => {
    render(
      <DashboardAuthGuard>
        <div>private dashboard</div>
      </DashboardAuthGuard>,
    );

    expect(screen.queryByText('private dashboard')).not.toBeInTheDocument();
    await waitFor(() => {
      expect(replaceMock).toHaveBeenCalledWith(
        '/setting?force_password_change=1',
      );
    });
  });

  it('renders the settings page where the password can be replaced', () => {
    navigationState.pathname = '/setting';

    render(
      <DashboardAuthGuard>
        <div>password settings</div>
      </DashboardAuthGuard>,
    );

    expect(screen.getByText('password settings')).toBeInTheDocument();
    expect(replaceMock).not.toHaveBeenCalled();
  });
});
