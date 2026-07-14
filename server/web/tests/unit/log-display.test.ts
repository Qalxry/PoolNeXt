import { describe, expect, it } from 'vitest';

import {
  formatLogLine,
  getClassificationMeta,
  getLevelMeta,
  matchesLogKeyword,
} from '@/features/logs/lib/log-display';
import type { AppLogItem } from '@/features/logs/types';

const baseItem: AppLogItem = {
  id: 1,
  classification: 'system',
  level: 'info',
  event: 'server_started',
  source: 'backend',
  message: 'Server started',
  details: { version: '1.0.0' },
  created_at: '2026-03-21T08:30:45.123Z',
};

describe('log display utils', () => {
  it('returns known labels for supported enums', () => {
    expect(getClassificationMeta('system').label).toBe('系统日志');
    expect(getLevelMeta('warn').label).toBe('WARN');
  });

  it('falls back gracefully for unknown enums', () => {
    expect(getClassificationMeta('legacy').label).toBe('未知分类(legacy)');
    expect(getLevelMeta('fatal').label).toBe('UNKNOWN(FATAL)');
  });

  it('formats log lines without throwing on unknown values', () => {
    expect(
      formatLogLine({
        ...baseItem,
        classification: 'legacy',
        level: 'fatal',
      }),
    ).toContain(
      '未知分类(legacy) | UNKNOWN(FATAL) | 后端 | server_started | Server started | version=1.0.0',
    );
  });

  it('matches keyword against raw values and derived labels', () => {
    expect(
      matchesLogKeyword({ ...baseItem, classification: 'legacy' }, 'legacy'),
    ).toBe(true);
    expect(matchesLogKeyword(baseItem, '系统日志')).toBe(true);
    expect(matchesLogKeyword({ ...baseItem, level: 'fatal' }, 'unknown')).toBe(
      true,
    );
  });
});
