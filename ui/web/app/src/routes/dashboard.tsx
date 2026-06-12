import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";
import { Card } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";

export const dashboardRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/dashboard",
  staticData: { title: "Dashboard" },
  component: DashboardPage,
});

const PLACEHOLDER_METRICS = ["Messages (24h)", "Pending backlog", "Delivery error rate", "Tenants"];

function DashboardPage() {
  return (
    <div className="grid gap-5">
      <PageHead title="Dashboard" />
      <section aria-label="Key metrics" className="grid grid-cols-4 gap-4 max-xl:grid-cols-2 max-sm:grid-cols-1">
        {PLACEHOLDER_METRICS.map((label) => (
          <Card key={label} className="grid content-start gap-2 px-5 py-4">
            <span className="text-sm text-muted">{label}</span>
            <Skeleton className="h-8 w-20" />
          </Card>
        ))}
      </section>
    </div>
  );
}

export const dashboardTree = dashboardRoute;
