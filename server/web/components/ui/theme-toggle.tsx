'use client';

import { Laptop, Moon, Sun } from 'lucide-react';

import { cn } from '@/lib/utils/cn';
import { themeModes, type ThemeMode } from '@/lib/theme/theme';
import { useTheme } from '@/components/providers/theme-provider';

interface ThemeToggleProps {
  className?: string;
}

export function ThemeToggle({ className }: ThemeToggleProps) {
  const { themeMode, resolvedTheme, setThemeMode } = useTheme();
  const currentIndex = themeModes.indexOf(themeMode);
  const nextTheme = themeModes[
    (currentIndex + 1) % themeModes.length
  ] as ThemeMode;

  const Icon =
    themeMode === 'light' ? Sun : themeMode === 'dark' ? Moon : Laptop;

  const label =
    themeMode === 'light'
      ? '浅色模式'
      : themeMode === 'dark'
        ? '深色模式'
        : '跟随系统';

  return (
    <button
      type="button"
      onClick={() => setThemeMode(nextTheme)}
      className={cn(
        'inline-flex h-8 w-8 items-center justify-center rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] text-[var(--foreground-secondary)] transition hover:bg-[var(--control-background-hover)] hover:text-[var(--foreground-primary)]',
        className,
      )}
      aria-label={`当前${label}，点击切换到${nextTheme === 'light' ? '浅色模式' : nextTheme === 'dark' ? '深色模式' : '跟随系统'}`}
      title={`${label}（当前生效：${resolvedTheme === 'dark' ? '深色' : '浅色'}），点击切换到${nextTheme === 'light' ? '浅色模式' : nextTheme === 'dark' ? '深色模式' : '跟随系统'}`}
    >
      <Icon className="h-4 w-4" aria-hidden="true" />
    </button>
  );
}
