import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

export function PageHead({
  title,
  className,
  children,
}: {
  title: string;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "grid-texture flex items-center gap-3 rounded-lg border border-border-soft px-4 py-3",
        className,
      )}
    >
      <h1 className="font-display text-xl font-semibold leading-tight tracking-[-0.015em]">
        {title}
      </h1>
      {children}
    </div>
  );
}
