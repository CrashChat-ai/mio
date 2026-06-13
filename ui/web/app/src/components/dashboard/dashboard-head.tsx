import { Link } from "@tanstack/react-router";
import { RotateCw } from "lucide-react";
import { Button } from "../ui/button";
import { StatusBadge } from "../status-badge";

export function DashboardHead({
  lagging,
  onRefresh,
}: {
  lagging: number;
  onRefresh: () => void;
}) {
  return (
    <div className="grid-texture flex flex-wrap items-center gap-3 rounded-lg border border-border-soft px-4 py-3">
      <h1 className="font-display text-xl font-semibold leading-tight tracking-[-0.015em]">
        Dashboard
      </h1>
      <span className="inline-flex min-h-[22px] items-center gap-2 rounded-sm border border-border bg-surface px-2 font-mono text-xs text-fg-2 before:size-1.5 before:rounded-full before:bg-muted before:content-['']">
        local · dev
      </span>
      {lagging > 0 ? (
        <Link
          to="/health"
          hash="stream-health"
          className="inline-flex min-h-[22px] items-center rounded-sm border border-warn/30 bg-warn/10 px-2 no-underline hover:bg-warn/15"
        >
          <StatusBadge variant="warn">gateway degraded</StatusBadge>
        </Link>
      ) : (
        <span className="inline-flex min-h-[22px] items-center rounded-sm border border-success/30 bg-success/10 px-2">
          <StatusBadge variant="ok">gateway healthy</StatusBadge>
        </span>
      )}
      <div className="ml-auto flex items-center gap-3">
        <Button variant="secondary" onClick={onRefresh}>
          <RotateCw size={14} />
          Refresh
        </Button>
      </div>
    </div>
  );
}
