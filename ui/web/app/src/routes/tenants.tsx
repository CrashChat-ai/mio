import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const tenantsRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/tenants",
  staticData: { title: "Tenants" },
  component: TenantsPage,
});

function TenantsPage() {
  return <PageHead title="Tenants" />;
}

export const tenantsTree = tenantsRoute;
