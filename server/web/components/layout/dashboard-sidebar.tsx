'use client';

import { useEffect } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Download,
  FileText,
  Gauge,
  House,
  Network,
  ScrollText,
  Server,
  Settings,
  Users,
  type LucideIcon,
} from 'lucide-react';

import { dashboardNavigation } from '@/lib/constants/navigation';
import { cn } from '@/lib/utils/cn';
import { isNavigationItemActive } from '@/lib/utils/navigation';
import { useAppShellStore } from '@/store/app-shell';
import type { NavigationIconKey, NavigationItem } from '@/types/navigation';

const sidebarIcons: Record<NavigationIconKey, LucideIcon> = {
  home: House,
  import: Download,
  node: Server,
  workspace: Network,
  runtime: Gauge,
  log: ScrollText,
  file: FileText,
  user: Users,
  setting: Settings,
};

function SidebarIcon({ icon }: { icon: NavigationIconKey }) {
  const Icon = sidebarIcons[icon];
  return <Icon className="h-4 w-4" aria-hidden="true" />;
}

function SidebarNavItem({
  item,
  currentPath,
  isSidebarCollapsed,
  forceExpanded,
  onNavigate,
  depth = 0,
}: {
  item: NavigationItem;
  currentPath: string;
  isSidebarCollapsed: boolean;
  forceExpanded?: boolean;
  onNavigate?: () => void;
  depth?: number;
}) {
  const active = isNavigationItemActive(currentPath, item);
  const hasChildren = Boolean(item.children?.length);
  const showLabel = forceExpanded || !isSidebarCollapsed;

  return (
    <div>
      <Link
        href={item.href}
        onClick={onNavigate}
        title={!showLabel ? item.label : undefined}
        className={cn(
          'flex h-8 items-center gap-2 rounded-[var(--radius-control)] border border-transparent px-2 text-xs transition-colors',
          depth > 0 && 'ml-2',
          active
            ? 'bg-[var(--brand-primary-soft)] font-medium text-[var(--brand-primary)]'
            : 'text-[var(--foreground-secondary)] hover:bg-[var(--control-background-hover)] hover:text-[var(--foreground-primary)]',
        )}
      >
        <span className="flex h-5 w-5 shrink-0 items-center justify-center">
          <SidebarIcon icon={item.icon} />
        </span>
        {showLabel ? (
          <span className="min-w-0 flex-1 truncate">{item.label}</span>
        ) : null}
      </Link>
      {showLabel && hasChildren ? (
        <div className="mt-0.5 space-y-0.5">
          {item.children?.map((child) => (
            <SidebarNavItem
              key={child.href}
              item={child}
              currentPath={currentPath}
              isSidebarCollapsed={isSidebarCollapsed}
              forceExpanded={forceExpanded}
              onNavigate={onNavigate}
              depth={depth + 1}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function SidebarContent({
  currentPath,
  isSidebarCollapsed,
  forceExpanded = false,
  onNavigate,
}: {
  currentPath: string;
  isSidebarCollapsed: boolean;
  forceExpanded?: boolean;
  onNavigate?: () => void;
}) {
  const showLabel = forceExpanded || !isSidebarCollapsed;

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 border-b border-[var(--border-default)] px-2">
        <div className="flex h-6 w-6 items-center justify-center rounded-[4px] bg-[var(--brand-primary)] text-[10px] font-bold text-[var(--foreground-inverse)]">
          PN
        </div>
        {showLabel ? (
          <p className="truncate text-[13px] font-semibold text-[var(--foreground-primary)]">
            PoolNeXt
          </p>
        ) : null}
      </div>

      <nav className="min-h-0 flex-1 px-1.5 py-2">
        <div className="flex max-h-full min-h-0 flex-col gap-0.5 overflow-y-auto">
          {dashboardNavigation.map((item) => (
            <SidebarNavItem
              key={item.href}
              item={item}
              currentPath={currentPath}
              isSidebarCollapsed={isSidebarCollapsed}
              forceExpanded={forceExpanded}
              onNavigate={onNavigate}
            />
          ))}
        </div>
      </nav>
    </div>
  );
}

export function DashboardSidebar() {
  const pathname = usePathname();
  const currentPath = pathname ?? '/';
  const isSidebarCollapsed = useAppShellStore(
    (state) => state.isSidebarCollapsed,
  );
  const isMobileSidebarOpen = useAppShellStore(
    (state) => state.isMobileSidebarOpen,
  );
  const setMobileSidebarOpen = useAppShellStore(
    (state) => state.setMobileSidebarOpen,
  );

  useEffect(() => {
    setMobileSidebarOpen(false);
  }, [currentPath, setMobileSidebarOpen]);

  return (
    <>
      <div
        className={cn(
          'fixed inset-0 z-30 bg-black/35 transition-opacity duration-200 min-[1000px]:hidden',
          isMobileSidebarOpen ? 'opacity-100' : 'pointer-events-none opacity-0',
        )}
        onClick={() => setMobileSidebarOpen(false)}
        aria-hidden="true"
      />

      <aside
        className={cn(
          'fixed top-0 left-0 z-40 h-screen w-[196px] overflow-hidden border-r border-[var(--border-default)] bg-[var(--surface-panel)] transition-transform duration-150 min-[1000px]:hidden',
          isMobileSidebarOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <SidebarContent
          currentPath={currentPath}
          isSidebarCollapsed={false}
          forceExpanded
          onNavigate={() => setMobileSidebarOpen(false)}
        />
      </aside>

      <aside
        className={cn(
          'sticky top-0 z-10 hidden h-screen shrink-0 overflow-hidden border-r border-[var(--border-default)] bg-[var(--surface-panel)] transition-[width] duration-150 min-[1000px]:block',
          isSidebarCollapsed ? 'w-[52px]' : 'w-[196px]',
        )}
      >
        <SidebarContent
          currentPath={currentPath}
          isSidebarCollapsed={isSidebarCollapsed}
        />
      </aside>
    </>
  );
}
