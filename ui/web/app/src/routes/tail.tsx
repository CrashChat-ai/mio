import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const tailRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/tail",
  staticData: { title: "Live tail" },
  component: TailPage,
});

function TailPage() {
  return <PageHead title="Live tail" />;
}

export const tailTree = tailRoute;
