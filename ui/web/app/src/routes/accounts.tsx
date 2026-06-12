import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const accountsRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/accounts",
  staticData: { title: "Accounts" },
  component: AccountsPage,
});

function AccountsPage() {
  return <PageHead title="Accounts" />;
}

export const accountsTree = accountsRoute;
