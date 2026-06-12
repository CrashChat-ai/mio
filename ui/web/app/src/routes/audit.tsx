import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const auditRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/audit",
  staticData: { title: "Audit" },
  component: AuditPage,
});

function AuditPage() {
  return <PageHead title="Audit" />;
}

export const auditTree = auditRoute;
