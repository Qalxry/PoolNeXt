import { apiRequest } from '@/lib/api/client';

import type {
  AppLogList,
  LogClassification,
  LogLevel,
} from '@/features/logs/types';

type GetLogsParams = {
  classification?: LogClassification | 'all';
  level?: LogLevel | 'all';
  source?: string | 'all';
  keyword?: string;
  afterId?: number;
  limit?: number;
};

export function getLogs({
  classification = 'all',
  level = 'all',
  source = 'all',
  keyword = '',
  afterId = 0,
  limit = 100,
}: GetLogsParams = {}) {
  const searchParams = new URLSearchParams({
    limit: String(limit),
  });

  if (afterId > 0) {
    searchParams.set('after_id', String(afterId));
  }

  if (classification !== 'all') {
    searchParams.set('classification', classification);
  }
  if (level !== 'all') {
    searchParams.set('level', level);
  }
  if (source !== 'all') {
    searchParams.set('source', source);
  }
  if (keyword.trim()) {
    searchParams.set('keyword', keyword.trim());
  }

  return apiRequest<AppLogList>(`/log/?${searchParams.toString()}`);
}

export function pushLog(
  classification: LogClassification,
  level: LogLevel,
  message: string,
) {
  return apiRequest<void>('/log/', {
    method: 'POST',
    body: JSON.stringify({
      classification,
      level,
      message,
    }),
  });
}
