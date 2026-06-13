import { Link, type LinkProps } from "@tanstack/react-router";
import { cn } from "../../lib/cn";
import { Card } from "../ui/card";
import { Sparkline } from "./sparkline";

export type StatCardProps = {
  label: string;
  value: string;
  delta?: string;
  series: number[];
  alert?: boolean;
  placeholder?: boolean;
  drillTo?: LinkProps["to"];
  drillHash?: string;
  drillLabel?: string;
};

export function StatCard({
  label,
  value,
  delta,
  series,
  alert,
  placeholder,
  drillTo,
  drillHash,
  drillLabel,
}: StatCardProps) {
  return (
    <Card className="grid content-start gap-2 px-5 py-4">
      <span className="text-sm text-muted">{label}</span>
      <div className="flex min-h-8 items-end justify-between gap-3">
        <strong
          className={cn(
            "block font-display text-2xl font-semibold leading-tight tracking-[-0.015em] tabular-nums",
            alert && "text-warn",
            placeholder && "text-muted",
          )}
        >
          {value}
        </strong>
        <Sparkline series={series} tone={alert ? "alert" : placeholder ? "flat" : "muted"} />
      </div>
      <div className="flex items-center justify-between gap-2">
        {delta && <p className="truncate font-mono text-xs text-muted">{delta}</p>}
        {drillTo && (
          <Link
            to={drillTo}
            hash={drillHash}
            className="ml-auto font-mono text-xs text-accent hover:text-accent-hover"
          >
            {drillLabel ?? "View"} →
          </Link>
        )}
      </div>
    </Card>
  );
}
