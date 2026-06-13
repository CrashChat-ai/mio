import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/cn";

const badgeVariants = cva(
  "inline-flex items-center gap-2 rounded-sm border px-2 py-0.5 font-mono text-xs font-medium",
  {
    variants: {
      variant: {
        neutral: "border-border bg-surface text-fg-2",
        accent: "border-accent/30 bg-accent/10 text-accent",
        success: "border-success/30 bg-success/10 text-success",
        warn: "border-warn/30 bg-warn/10 text-warn",
        danger: "border-danger/30 bg-danger/10 text-danger",
      },
    },
    defaultVariants: {
      variant: "neutral",
    },
  },
);

type BadgeProps = React.ComponentProps<"span"> & VariantProps<typeof badgeVariants>;

function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant, className }))} {...props} />;
}

export { Badge, badgeVariants };
