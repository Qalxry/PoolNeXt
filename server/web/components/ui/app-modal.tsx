'use client';

import { useEffect, type ReactNode } from 'react';
import { X } from 'lucide-react';

import { cn } from '@/lib/utils/cn';

interface AppModalProps {
  isOpen: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  size?: 'md' | 'lg' | 'xl';
}

const sizeClassNameMap = {
  md: 'max-w-2xl',
  lg: 'max-w-4xl',
  xl: 'max-w-5xl',
} satisfies Record<NonNullable<AppModalProps['size']>, string>;

export function AppModal({
  isOpen,
  title,
  description,
  children,
  footer,
  onClose,
  size = 'lg',
}: AppModalProps) {
  useEffect(() => {
    if (!isOpen) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose();
      }
    };

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    window.addEventListener('keydown', handleKeyDown);

    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose]);

  if (!isOpen) {
    return null;
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-3 py-4"
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <button
        type="button"
        className="absolute inset-0 cursor-default"
        onClick={onClose}
        aria-label="关闭弹窗"
      />
      <div
        className={cn(
          'relative flex max-h-[calc(100vh-2rem)] w-full flex-col overflow-hidden rounded-[var(--radius-panel)] border border-[var(--border-strong)] bg-[var(--surface-panel)] shadow-[var(--shadow-lg)]',
          sizeClassNameMap[size],
        )}
      >
        <div className="flex items-start justify-between gap-3 border-b border-[var(--border-default)] bg-[var(--surface-base)] px-4 py-3">
          <div>
            <h2 className="text-base font-semibold text-[var(--foreground-primary)]">
              {title}
            </h2>
            {description ? (
              <p className="mt-0.5 max-w-2xl text-xs leading-4 text-[var(--foreground-secondary)]">
                {description}
              </p>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-control)] border border-[var(--border-default)] bg-[var(--control-background)] text-[var(--foreground-primary)] transition hover:bg-[var(--control-background-hover)]"
            aria-label="关闭弹窗"
          >
            <X className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-3">{children}</div>
        {footer ? (
          <div className="border-t border-[var(--border-default)] bg-[var(--surface-base)] px-4 py-3">
            {footer}
          </div>
        ) : null}
      </div>
    </div>
  );
}
