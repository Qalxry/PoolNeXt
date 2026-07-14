'use client';

import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';

import { LoadingState } from '@/components/feedback/loading-state';
import { useAuth } from '@/components/providers/auth-provider';
import { getSafeInternalRedirect } from '@/lib/security/redirect';

interface PublicAuthGuardProps {
  children: ReactNode;
}

export function PublicAuthGuard({ children }: PublicAuthGuardProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { isAuthenticated, isLoading, user } = useAuth();

  useEffect(() => {
    if (!isLoading && isAuthenticated) {
      router.replace(
        user?.must_change_password
          ? '/setting?force_password_change=1'
          : getSafeInternalRedirect(searchParams?.get('redirect')),
      );
    }
  }, [isAuthenticated, isLoading, router, searchParams, user]);

  if (isLoading) {
    return <LoadingState />;
  }

  if (isAuthenticated) {
    return null;
  }

  return <>{children}</>;
}
