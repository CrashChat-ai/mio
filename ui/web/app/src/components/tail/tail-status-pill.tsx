import { StatusBadge, type StatusVariant } from "../status-badge";
import { cn } from "../../lib/cn";
import type { TailStatus } from "./use-sse-tail";

const LABEL: Record<TailStatus, string> = {
  idle: "idle",
  streaming: "streaming",
  paused: "paused",
  reconnecting: "reconnecting",
  error: "error",
};

const VARIANT: Record<TailStatus, StatusVariant> = {
  idle: "neutral",
  streaming: "ok",
  paused: "neutral",
  reconnecting: "warn",
  error: "danger",
};

export function TailStatusPill({ status }: { status: TailStatus }) {
  return (
    <StatusBadge variant={VARIANT[status]}>
      <span
        aria-hidden="true"
        className={cn(
          "-ml-1 size-1.5 rounded-full",
          status === "streaming" && "animate-pulse bg-success",
        )}
      />
      {LABEL[status]}
    </StatusBadge>
  );
}
