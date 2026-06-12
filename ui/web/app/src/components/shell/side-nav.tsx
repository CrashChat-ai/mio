import type { PointerEvent as ReactPointerEvent } from "react";
import { Link, useMatchRoute } from "@tanstack/react-router";
import {
  Activity,
  Building2,
  ChevronsLeft,
  LayoutGrid,
  Rocket,
  Shapes,
  ShieldCheck,
  Terminal,
  Users,
  type LucideIcon,
} from "lucide-react";
import { cn } from "../../lib/cn";

type NavTo =
  | "/dashboard"
  | "/tenants"
  | "/accounts"
  | "/onboarding"
  | "/health"
  | "/tail"
  | "/channel-types"
  | "/audit";

type NavSection = { label: string; items: Array<{ to: NavTo; label: string; icon: LucideIcon }> };

const SECTIONS: NavSection[] = [
  { label: "Overview", items: [{ to: "/dashboard", label: "Dashboard", icon: LayoutGrid }] },
  {
    label: "Tenancy",
    items: [
      { to: "/tenants", label: "Tenants", icon: Building2 },
      { to: "/accounts", label: "Accounts", icon: Users },
      { to: "/onboarding", label: "Onboarding", icon: Rocket },
    ],
  },
  {
    label: "Operations",
    items: [
      { to: "/health", label: "Stream health", icon: Activity },
      { to: "/tail", label: "Live tail", icon: Terminal },
    ],
  },
  {
    label: "Reference",
    items: [
      { to: "/channel-types", label: "Channel types", icon: Shapes },
      { to: "/audit", label: "Audit", icon: ShieldCheck },
    ],
  },
];

type SideNavProps = {
  collapsed: boolean;
  onToggle: () => void;
  onResizeStart: (event: ReactPointerEvent) => void;
};

/* phone (<560px): this rail maps to a Sheet/offcanvas — lands with the responsive pass */
export function SideNav({ collapsed, onToggle, onResizeStart }: SideNavProps) {
  return (
    <nav
      aria-label="Primary"
      className="relative sticky top-0 flex h-dvh flex-col overflow-y-auto border-r border-border px-3 py-4"
    >
      <div className={cn("flex items-center gap-3 px-2 pb-5", collapsed && "justify-center px-0")}>
        <span
          aria-hidden="true"
          className="grid size-6 shrink-0 place-items-center rounded-sm bg-accent text-accent-on"
        >
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2.4">
            <path d="M2.5 14V2.5L8 9l5.5-6.5V14" />
          </svg>
        </span>
        <span className={cn("leading-tight", collapsed && "sr-only")}>
          <span className="block font-display text-sm font-semibold tracking-wide">MIO</span>
          <span className="block font-mono text-xs text-muted">operator console</span>
        </span>
      </div>
      <div className="grid gap-4">
        {SECTIONS.map((section) => (
          <div key={section.label} className="grid gap-1">
            <span className={cn("eyebrow px-2", collapsed && "sr-only")}>{section.label}</span>
            {section.items.map((item) => (
              <NavItem key={item.to} {...item} collapsed={collapsed} />
            ))}
          </div>
        ))}
      </div>
      <div className="mt-auto border-t border-border-soft pt-4">
        <button
          type="button"
          aria-pressed={collapsed}
          onClick={onToggle}
          className={cn(
            "flex min-h-8 w-full cursor-pointer items-center gap-3 rounded-md px-2 text-sm font-medium text-muted transition-colors hover:bg-surface-2 hover:text-fg",
            collapsed && "justify-center px-0",
          )}
        >
          <ChevronsLeft size={16} className={cn("shrink-0 transition-transform", collapsed && "rotate-180")} />
          <span className={cn(collapsed && "sr-only")}>
            {collapsed ? "Expand navigation" : "Collapse navigation"}
          </span>
        </button>
      </div>
      <div
        role="presentation"
        onPointerDown={onResizeStart}
        className="absolute inset-y-0 right-0 w-1 cursor-col-resize hover:bg-accent/30"
      />
    </nav>
  );
}

function NavItem({
  to,
  label,
  icon: Icon,
  collapsed,
}: {
  to: NavTo;
  label: string;
  icon: LucideIcon;
  collapsed: boolean;
}) {
  const matchRoute = useMatchRoute();
  const active = Boolean(matchRoute({ to, fuzzy: true }));
  return (
    <Link
      to={to}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex min-h-8 items-center gap-3 rounded-md px-2 text-sm font-medium text-fg-2 transition-colors hover:bg-surface-2 hover:text-fg",
        active && "bg-accent/10 text-accent shadow-[inset_2px_0_0_var(--color-accent)] hover:bg-accent/10 hover:text-accent",
        collapsed && "justify-center px-0",
      )}
    >
      <Icon size={16} className="shrink-0" />
      <span className={cn(collapsed && "sr-only")}>{label}</span>
    </Link>
  );
}
