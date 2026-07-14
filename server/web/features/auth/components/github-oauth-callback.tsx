'use client';

import { useMutation } from '@tanstack/react-query';
import { useRouter, useSearchParams } from 'next/navigation';
import { useEffect, useRef, useState } from 'react';

import { InlineMessage } from '@/components/feedback/inline-message';
import { LoadingState } from '@/components/feedback/loading-state';
import { useAuth } from '@/components/providers/auth-provider';
import { AppCard } from '@/components/ui/app-card';
import { exchangeGitHubCode } from '@/features/auth/api/auth';
import { getSafeInternalRedirect } from '@/lib/security/redirect';

export function GitHubOAuthCallback() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { setUser } = useAuth();
  const processedCallback = useRef('');
  const [prompt, setPrompt] = useState('正在处理 GitHub 授权结果...');
  const [message, setMessage] = useState<{
    tone: 'danger' | 'success';
    text: string;
  } | null>(null);

  const mutation = useMutation({
    mutationFn: ({ code, state }: { code: string; state: string }) =>
      exchangeGitHubCode(code, state),
    onSuccess: (result) => {
      setUser(result.user);
      setMessage({
        tone: 'success',
        text:
          result.mode === 'bind'
            ? 'GitHub 账号绑定成功，正在跳转...'
            : '登录成功，正在跳转...',
      });
      router.replace(
        result.user.must_change_password
          ? '/setting?force_password_change=1'
          : getSafeInternalRedirect(result.redirect),
      );
    },
    onError: (error: Error) => {
      setPrompt('授权处理失败');
      setMessage({
        tone: 'danger',
        text: error.message || 'GitHub 授权失败，请稍后重试。',
      });
    },
  });

  useEffect(() => {
    const code = searchParams?.get('code');
    const state = searchParams?.get('state');
    if (!code || !state) {
      setPrompt('缺少授权参数');
      setMessage({
        tone: 'danger',
        text: '未收到完整的 GitHub 授权参数，请返回登录页重试。',
      });
      return;
    }

    const callbackKey = `${code}\u0000${state}`;
    if (processedCallback.current === callbackKey) {
      return;
    }
    processedCallback.current = callbackKey;
    mutation.mutate({ code, state });
  }, [mutation, searchParams]);

  return (
    <AppCard title="GitHub OAuth 回调" description={prompt}>
      <div className="space-y-2.5">
        {mutation.isPending ? <LoadingState /> : null}
        {message ? (
          <InlineMessage tone={message.tone} message={message.text} />
        ) : null}
      </div>
    </AppCard>
  );
}
