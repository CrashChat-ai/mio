import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "../__root";

export const tenantsRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/tenants",
  staticData: { title: "Tenants" },
});
