import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type StatusVariant = "ok" | "warn" | "danger" | "neutral";

const TEXT_CLASS: Record<StatusVariant, string> = {
  ok: "text-fg-2",
  warn: "text-warn",
  danger: "text-danger",
  neutral: "text-fg-2",
};

const DOT_CLASS: Record<StatusVariant, string> = {
  ok: "bg-success",
  warn: "bg-warn",
  danger: "bg-danger",
  neutral: "bg-muted",
};

export function statusVariant(status: string): StatusVariant {
  const normalized = status.toLowerCase();
  if (normalized === "active" || normalized === "enabled" || normalized === "ok") return "ok";
  if (normalized === "disabled" || normalized === "error" || normalized === "failed") return "danger";
  if (normalized === "pending" || normalized === "degraded") return "warn";
  return "neutral";
}

export function StatusBadge({
  variant = "neutral",
  className,
  children,
}: {
  variant?: StatusVariant;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 font-mono text-xs font-semibold uppercase leading-none tracking-[0.08em]",
        TEXT_CLASS[variant],
        className,
      )}
    >
      <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", DOT_CLASS[variant])} />
      {children}
    </span>
  );
}
