import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export function EmptyState({
  title,
  description,
  action,
  className,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "grid-texture grid place-items-center gap-2 rounded-lg border border-border-soft px-6 py-12 text-center",
        className,
      )}
    >
      <p className="font-display text-base font-semibold">{title}</p>
      {description && <p className="mx-auto max-w-sm text-sm text-muted">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}
