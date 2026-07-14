import type {
  AppLogItem,
  LogClassification,
  LogLevel,
} from '@/features/logs/types';
import {
  formatDateTime,
  formatDateTimeWithMilliseconds,
} from '@/lib/utils/date';

type ClassificationMeta = {
  label: string;
  variant: 'success' | 'warning' | 'danger' | 'info';
};

type LevelMeta = {
  label: string;
  variant: 'success' | 'warning' | 'danger' | 'info';
};

const classificationMeta: Record<LogClassification, ClassificationMeta> = {
  system: { label: '系统日志', variant: 'info' },
  business: { label: '业务日志', variant: 'success' },
  security: { label: '安全日志', variant: 'danger' },
};

const levelMeta: Record<LogLevel, LevelMeta> = {
  debug: { label: 'DEBUG', variant: 'info' },
  info: { label: 'INFO', variant: 'success' },
  warn: { label: 'WARN', variant: 'warning' },
  error: { label: 'ERROR', variant: 'danger' },
};

const sourceLabels: Record<string, string> = {
  backend: '后端',
  frontend: '前端',
  mihomo: 'Mihomo',
  scheduler: '调度器',
};

function isLogClassification(value: string): value is LogClassification {
  return value in classificationMeta;
}

function isLogLevel(value: string): value is LogLevel {
  return value in levelMeta;
}

export function getClassificationMeta(classification: string) {
  if (isLogClassification(classification)) {
    return classificationMeta[classification];
  }
  return {
    label: classification ? `未知分类(${classification})` : '未知分类',
    variant: 'warning' as const,
  };
}

export function getLevelMeta(level: string) {
  if (isLogLevel(level)) {
    return levelMeta[level];
  }
  return {
    label: level ? `UNKNOWN(${level.toUpperCase()})` : 'UNKNOWN',
    variant: 'warning' as const,
  };
}

export function getSourceLabel(source: string) {
  return (sourceLabels[source] ?? source) || '未知来源';
}

export function formatLogLine(item: AppLogItem) {
  const details = Object.entries(item.details ?? {})
    .map(([key, value]) => `${key}=${value}`)
    .join(' | ');
  return [
    formatDateTimeWithMilliseconds(item.created_at),
    getClassificationMeta(item.classification).label,
    getLevelMeta(item.level).label,
    getSourceLabel(item.source),
    item.event || 'general',
    item.message,
    details,
  ]
    .filter(Boolean)
    .join(' | ');
}

export function matchesLogKeyword(item: AppLogItem, keyword: string) {
  const normalizedKeyword = keyword.trim().toLowerCase();
  if (!normalizedKeyword) {
    return true;
  }

  const detailText = Object.entries(item.details ?? {})
    .map(([key, value]) => `${key} ${value}`)
    .join(' ')
    .toLowerCase();
  return [
    item.message,
    item.event,
    item.source,
    getSourceLabel(item.source),
    item.classification,
    getClassificationMeta(item.classification).label,
    item.level,
    getLevelMeta(item.level).label,
    detailText,
    formatDateTime(item.created_at),
    formatDateTimeWithMilliseconds(item.created_at),
  ].some((value) => value.toLowerCase().includes(normalizedKeyword));
}
