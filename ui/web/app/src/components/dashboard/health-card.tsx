import { Link } from "@tanstack/react-router";
import type { ConsumerHealth } from "../../lib/api/types";
import { StatusBadge } from "../status-badge";
import { StreamHealthTable } from "../health/stream-health-table";
import { laggingCount } from "../health/stream-health-query";

export function HealthCard({
  consumers,
  isLoading,
  error,
}: {
  consumers: ConsumerHealth[];
  isLoading?: boolean;
  error?: Error | null;
}) {
  const lagging = laggingCount(consumers);

  return (
    <section
      id="stream-health"
      aria-label="Stream health"
      className="scroll-mt-16 overflow-hidden rounded-lg border border-border bg-surface shadow-elev-raised"
    >
      <header className="flex items-center justify-between gap-4 border-b border-border-soft px-5 py-4">
        <div>
          <p className="eyebrow">jetstream</p>
          <h2 className="font-display text-base font-semibold leading-tight">Stream health</h2>
        </div>
        {lagging > 0 ? (
          <StatusBadge variant="warn">
            {lagging} of {consumers.length} consumers lagging
          </StatusBadge>
        ) : (
          <StatusBadge variant="ok">all consumers healthy</StatusBadge>
        )}
      </header>
      <StreamHealthTable consumers={consumers} isLoading={isLoading} error={error} />
      <footer className="flex justify-end border-t border-border-soft px-5 py-3">
        <Link to="/health" className="font-mono text-xs text-accent hover:text-accent-hover">
          View all →
        </Link>
      </footer>
    </section>
  );
}
