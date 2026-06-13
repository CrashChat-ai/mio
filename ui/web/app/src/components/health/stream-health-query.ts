import { queryOptions } from "@tanstack/react-query";
import { queries } from "../../lib/query-keys";
import type { ConsumerHealth } from "../../lib/api/types";

export const streamHealthQuery = queryOptions({
  ...queries.streamHealth.consumers,
  select: (data) => data.consumers,
});

export type HealthStatus = "ok" | "warn" | "lagging";

const STALE_MS = 5 * 60 * 1000;

export function consumerStatus(c: ConsumerHealth): HealthStatus {
  if (c.numPending > 0 || c.numAckPending > 0) return "lagging";
  if (c.lastDelivered && Date.now() - Date.parse(c.lastDelivered) > STALE_MS) return "warn";
  return "ok";
}

export function laggingCount(consumers: ConsumerHealth[]): number {
  return consumers.filter((c) => consumerStatus(c) !== "ok").length;
}

export function timeAgo(value?: string): string {
  if (!value) return "—";
  const delta = Date.now() - Date.parse(value);
  if (!Number.isFinite(delta) || delta < 0) return "—";
  const sec = Math.floor(delta / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}
