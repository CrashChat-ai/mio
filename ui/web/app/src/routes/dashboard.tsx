import { createRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { authedRoute } from "./__root";
import { tenantsListQuery } from "../queries/tenants";
import { laggingCount, streamHealthQuery } from "../components/health/stream-health-query";
import { DashboardHead } from "../components/dashboard/dashboard-head";
import { StatCard } from "../components/dashboard/stat-card";
import { HealthCard } from "../components/dashboard/health-card";
import { deriveStats } from "../components/dashboard/dashboard-stats";
import { OnboardingCta } from "../components/dashboard/onboarding-cta";
import { AuditTile } from "../components/dashboard/audit-tile";
import { LiveTailView } from "../components/tail/live-tail-view";

export const dashboardRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/dashboard",
  staticData: { title: "Dashboard" },
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(streamHealthQuery);
    void context.queryClient.prefetchQuery(tenantsListQuery);
  },
  component: DashboardPage,
});

function DashboardPage() {
  const health = useQuery(streamHealthQuery);
  const tenants = useQuery(tenantsListQuery);
  const consumers = health.data ?? [];
  const stats = deriveStats(consumers, tenants.data ?? []);

  return (
    <div className="grid gap-5">
      <DashboardHead lagging={laggingCount(consumers)} onRefresh={() => void health.refetch()} />
      <section
        aria-label="Key metrics"
        className="grid grid-cols-4 gap-4 max-xl:grid-cols-2 max-sm:grid-cols-1"
      >
        {stats.map((stat) => (
          <StatCard key={stat.label} {...stat} />
        ))}
      </section>
      <HealthCard consumers={consumers} isLoading={health.isLoading} error={health.error} />
      <div className="grid grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)] gap-4 max-[1100px]:grid-cols-1">
        <LiveTailView />
        <div className="grid grid-rows-[auto_1fr] gap-4">
          <OnboardingCta />
          <AuditTile />
        </div>
      </div>
    </div>
  );
}

export const dashboardTree = dashboardRoute;
