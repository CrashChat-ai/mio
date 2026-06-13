import type { ConsumerHealth, Tenant } from "../../lib/api/types";
import type { StatCardProps } from "./stat-card";

export function deriveStats(consumers: ConsumerHealth[], tenants: Tenant[]): StatCardProps[] {
  const pending = consumers.reduce((sum, c) => sum + c.numPending + c.numAckPending, 0);
  const lagging = consumers.filter((c) => c.numPending > 0 || c.numAckPending > 0);

  return [
    {
      label: "Messages (24h)",
      value: "—",
      delta: "metrics RPC pending",
      series: [],
      placeholder: true,
    },
    {
      label: "Pending backlog",
      value: String(pending),
      delta:
        pending > 0
          ? `${lagging.length} consumer${lagging.length === 1 ? "" : "s"} lagging`
          : "all caught up",
      series: consumers.map((c) => c.numPending + c.numAckPending),
      alert: pending > 0,
      drillTo: "/health",
      drillHash: "stream-health",
      drillLabel: "Inspect",
    },
    {
      label: "Delivery error rate",
      value: "—",
      delta: "metrics RPC pending",
      series: [],
      placeholder: true,
    },
    {
      label: "Tenants",
      value: String(tenants.length),
      delta: tenants.length === 0 ? "none yet" : "live",
      series: [tenants.length, tenants.length],
    },
  ];
}
