'use client';

import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { InlineMessage } from '@/components/feedback/inline-message';
import { useAuth } from '@/components/providers/auth-provider';
import { AppCard } from '@/components/ui/app-card';
import { beginGitHubOAuth, login } from '@/features/auth/api/auth';
import { getPublicStatus } from '@/features/auth/api/public';
import {
  AuthButton,
  AuthFormField,
  AuthInput,
  SecondaryButton,
} from '@/features/auth/components/auth-form-primitives';
import { PublicAuthGuard } from '@/features/auth/components/public-auth-guard';
import { getSafeInternalRedirect } from '@/lib/security/redirect';

const loginSchema = z.object({
  username: z.string().min(1, '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
});

type LoginFormValues = z.infer<typeof loginSchema>;

export function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { setUser } = useAuth();
  const [errorMessage, setErrorMessage] = useState('');
  const redirect = getSafeInternalRedirect(searchParams?.get('redirect'));

  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      username: '',
      password: '',
    },
  });

  const statusQuery = useQuery({
    queryKey: ['public-status'],
    queryFn: getPublicStatus,
  });

  const loginMutation = useMutation({
    mutationFn: login,
    onSuccess: (user) => {
      setUser(user);
      router.replace(
        user.must_change_password
          ? '/setting?force_password_change=1'
          : redirect,
      );
    },
    onError: (error: Error) => {
      setErrorMessage(error.message || '登录失败，请稍后重试。');
    },
  });

  const githubMutation = useMutation({
    mutationFn: () => beginGitHubOAuth('login', redirect),
    onSuccess: ({ authorize_url: authorizeURL }) => {
      window.location.assign(authorizeURL);
    },
    onError: (error: Error) => {
      setErrorMessage(error.message || 'GitHub 登录当前不可用。');
    },
  });

  const handleSubmit = form.handleSubmit((values) => {
    setErrorMessage('');
    loginMutation.mutate(values);
  });

  const handleGitHubLogin = () => {
    if (!statusQuery.data?.github_client_id) {
      setErrorMessage('GitHub 登录当前不可用。');
      return;
    }

    setErrorMessage('');
    githubMutation.mutate();
  };

  return (
    <PublicAuthGuard>
      <AppCard title="用户登录">
        <form className="space-y-2.5" onSubmit={handleSubmit}>
          <AuthFormField label="用户名">
            <AuthInput
              placeholder="请输入用户名"
              {...form.register('username')}
            />
            {form.formState.errors.username ? (
              <span className="text-xs text-[var(--status-danger-foreground)]">
                {form.formState.errors.username.message}
              </span>
            ) : null}
          </AuthFormField>

          <AuthFormField label="密码">
            <AuthInput
              type="password"
              placeholder="请输入密码"
              {...form.register('password')}
            />
            {form.formState.errors.password ? (
              <span className="text-xs text-[var(--status-danger-foreground)]">
                {form.formState.errors.password.message}
              </span>
            ) : null}
          </AuthFormField>

          {errorMessage ? (
            <InlineMessage tone="danger" message={errorMessage} />
          ) : null}

          <div className="flex flex-col gap-3 sm:flex-row">
            <AuthButton type="submit" disabled={loginMutation.isPending}>
              {loginMutation.isPending ? '登录中...' : '登录'}
            </AuthButton>
            {statusQuery.data?.github_oauth ? (
              <SecondaryButton
                type="button"
                onClick={handleGitHubLogin}
                disabled={githubMutation.isPending}
                className="w-full sm:w-auto"
              >
                {githubMutation.isPending ? '正在跳转...' : 'GitHub 登录'}
              </SecondaryButton>
            ) : null}
          </div>
        </form>

        <div className="mt-6 flex flex-wrap gap-3 text-sm text-[var(--foreground-secondary)]">
          <Link
            href="/reset"
            className="text-[var(--brand-primary)] transition hover:opacity-80"
          >
            忘记密码？
          </Link>
          <span>·</span>
          <Link
            href="/register"
            className="text-[var(--brand-primary)] transition hover:opacity-80"
          >
            注册
          </Link>
        </div>
      </AppCard>
    </PublicAuthGuard>
  );
}
