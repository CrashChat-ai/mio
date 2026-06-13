import type { ReactNode } from "react";
import { Check } from "lucide-react";
import { cn } from "../../lib/cn";
import type { StepStatus } from "./use-steps";

export type StepDefinition = {
  title: string;
  summary?: ReactNode;
  content: ReactNode;
};

const MARKER_CLASS: Record<StepStatus, string> = {
  done: "border-success/40 text-success",
  current: "border-accent/50 text-accent",
  locked: "border-border text-muted",
};

export function Steps({
  steps,
  statusOf,
}: {
  steps: StepDefinition[];
  statusOf: (index: number) => StepStatus;
}) {
  return (
    <ol aria-label="Onboarding steps" className="grid list-none gap-3 p-0">
      {steps.map((step, index) => {
        const status = statusOf(index);
        return (
          <li
            key={step.title}
            aria-current={status === "current" ? "step" : undefined}
            className={cn(
              "rounded-lg border px-4 py-3",
              status === "current" ? "border-border bg-surface-2/40" : "border-border-soft",
              status === "locked" && "opacity-60",
            )}
          >
            <div className="flex items-center gap-3">
              <span
                aria-hidden="true"
                className={cn(
                  "grid size-6 shrink-0 place-items-center rounded-full border font-mono text-xs font-semibold",
                  MARKER_CLASS[status],
                )}
              >
                {status === "done" ? <Check size={13} /> : index + 1}
              </span>
              <span className="font-display text-sm font-semibold">{step.title}</span>
              {status === "done" && step.summary && (
                <span className="min-w-0 truncate font-mono text-xs text-muted">
                  {step.summary}
                </span>
              )}
            </div>
            {status === "current" && <div className="mt-3 grid gap-3 pl-9">{step.content}</div>}
          </li>
        );
      })}
    </ol>
  );
}
