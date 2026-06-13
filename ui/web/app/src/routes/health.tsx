import { createRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";
import { Card } from "../components/ui/card";
import { StatusBadge } from "../components/status-badge";
import {
  RefetchIntervalProvider,
  useRefetchInterval,
} from "../components/health/refetch-interval";
import { RefetchControls } from "../components/health/refetch-controls";
import { StreamHealthTable } from "../components/health/stream-health-table";
import { laggingCount, streamHealthQuery } from "../components/health/stream-health-query";

export const healthRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/health",
  staticData: { title: "Stream health" },
  loader: ({ context }) => void context.queryClient.prefetchQuery(streamHealthQuery),
  component: HealthPage,
});

function HealthPage() {
  return (
    <RefetchIntervalProvider>
      <HealthContent />
    </RefetchIntervalProvider>
  );
}

function HealthContent() {
  const { refetchInterval } = useRefetchInterval();
  const {
    data: consumers = [],
    isLoading,
    error,
  } = useQuery({ ...streamHealthQuery, refetchInterval });
  const lagging = laggingCount(consumers);

  return (
    <div className="grid gap-5">
      <PageHead title="Stream health">
        <div className="ml-auto">
          <RefetchControls />
        </div>
      </PageHead>
      {lagging > 0 && (
        <Card className="flex items-center gap-3 border-warn/30 bg-warn/5 px-5 py-3 shadow-elev-ring">
          <StatusBadge variant="warn">
            {lagging} of {consumers.length} consumers lagging
          </StatusBadge>
          <span className="text-sm text-muted">
            Pending or ack-pending messages are accumulating on these consumers.
          </span>
        </Card>
      )}
      <Card className="overflow-hidden p-0 shadow-elev-raised">
        <StreamHealthTable consumers={consumers} isLoading={isLoading} error={error} />
      </Card>
    </div>
  );
}

export const healthTree = healthRoute;
