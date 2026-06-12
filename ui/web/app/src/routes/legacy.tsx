import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { rootRoute } from "./__root";

export const legacyRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/legacy",
  staticData: { title: "Legacy console" },
  component: lazyRouteComponent(() => import("../legacy-app")),
});
