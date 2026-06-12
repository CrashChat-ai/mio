import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const healthRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/health",
  staticData: { title: "Stream health" },
  component: HealthPage,
});

function HealthPage() {
  return <PageHead title="Stream health" />;
}

export const healthTree = healthRoute;
