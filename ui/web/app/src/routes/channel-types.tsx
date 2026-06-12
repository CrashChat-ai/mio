import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const channelTypesRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/channel-types",
  staticData: { title: "Channel types" },
  component: ChannelTypesPage,
});

function ChannelTypesPage() {
  return <PageHead title="Channel types" />;
}

export const channelTypesTree = channelTypesRoute;
