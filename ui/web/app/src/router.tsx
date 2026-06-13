import { createRoute, createRouter, redirect } from "@tanstack/react-router";
import { queryClient } from "./lib/query-client";
import { authedRoute, rootRoute } from "./routes/__root";
import { accountsTree } from "./routes/accounts";
import { auditTree } from "./routes/audit";
import { channelTypesTree } from "./routes/channel-types";
import { dashboardTree } from "./routes/dashboard";
import { healthTree } from "./routes/health";
import { loginRoute } from "./routes/login";
import { onboardingTree } from "./routes/onboarding";
import { tailTree } from "./routes/tail";
import { tenantsTree } from "./routes/tenants";

const indexRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/dashboard" });
  },
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  authedRoute.addChildren([
    indexRoute,
    dashboardTree,
    tenantsTree,
    accountsTree,
    onboardingTree,
    healthTree,
    tailTree,
    channelTypesTree,
    auditTree,
  ]),
]);

export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
  interface StaticDataRouteOption {
    title?: string;
  }
}
