import { cn } from "../../lib/cn";
import type { ConsumerHealth } from "../../lib/api/types";
import { StatusBadge, type StatusVariant } from "../status-badge";
import { DataTableSkeleton } from "../data-table/data-table-skeleton";
import { EmptyState } from "../empty-state";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../ui/table";
import { consumerStatus, timeAgo, type HealthStatus } from "./stream-health-query";

const LABEL: Record<HealthStatus, string> = { ok: "ok", warn: "stale", lagging: "lagging" };
const VARIANT: Record<HealthStatus, StatusVariant> = {
  ok: "ok",
  warn: "warn",
  lagging: "warn",
};

export function StreamHealthTable({
  consumers,
  isLoading,
  error,
}: {
  consumers: ConsumerHealth[];
  isLoading?: boolean;
  error?: Error | null;
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead>Consumer</TableHead>
          <TableHead>Stream</TableHead>
          <TableHead className="text-right">Pending</TableHead>
          <TableHead className="text-right">Ack pending</TableHead>
          <TableHead className="text-right">Last delivered</TableHead>
          <TableHead>Status</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {isLoading ? (
          <DataTableSkeleton columns={6} />
        ) : consumers.length === 0 ? (
          <TableRow className="hover:bg-transparent">
            <TableCell colSpan={6} className="whitespace-normal p-3">
              {error ? (
                <EmptyState title="Failed to load stream health" description={error.message} />
              ) : (
                <EmptyState
                  title="No consumers reporting"
                  description="JetStream consumer health appears here once streams are active."
                />
              )}
            </TableCell>
          </TableRow>
        ) : (
          consumers.map((c) => {
            const status = consumerStatus(c);
            const alert = status !== "ok";
            return (
              <TableRow key={`${c.stream}/${c.consumerName}`}>
                <TableCell className="font-medium text-fg">{c.consumerName}</TableCell>
                <TableCell className="font-mono text-xs tracking-[0.02em] text-fg-2">
                  {c.stream}
                </TableCell>
                <TableCell
                  className={cn("text-right font-mono text-xs text-fg-2", alert && "text-warn")}
                >
                  {c.numPending}
                </TableCell>
                <TableCell
                  className={cn("text-right font-mono text-xs text-fg-2", alert && "text-warn")}
                >
                  {c.numAckPending}
                </TableCell>
                <TableCell
                  className={cn("text-right font-mono text-xs text-fg-2", alert && "text-warn")}
                >
                  {timeAgo(c.lastDelivered)}
                </TableCell>
                <TableCell>
                  <StatusBadge variant={VARIANT[status]}>{LABEL[status]}</StatusBadge>
                </TableCell>
              </TableRow>
            );
          })
        )}
      </TableBody>
    </Table>
  );
}
