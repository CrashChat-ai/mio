import * as React from "react";
import { cn } from "../../lib/cn";

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      className={cn(
        "flex min-h-8 w-full rounded-md border border-border bg-surface px-3 text-sm text-fg placeholder:text-muted focus-visible:border-accent focus-visible:shadow-focus-ring focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
