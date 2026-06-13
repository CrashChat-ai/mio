import { createRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { authedRoute } from "./__root";
import { auditListQuery } from "../queries/audit";
import { auditColumns } from "../components/data-table/columns/audit-columns";
import { DataTable } from "../components/data-table/data-table";
import { EmptyState } from "../components/empty-state";
import { PageHead } from "../components/shell/page-head";
import { Button } from "../components/ui/button";

export const auditRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/audit",
  staticData: { title: "Audit" },
  loader: ({ context }) => void context.queryClient.prefetchQuery(auditListQuery),
  component: AuditPage,
});

function AuditPage() {
  const { data: events = [], isLoading, error, refetch } = useQuery(auditListQuery);

  return (
    <div className="grid gap-5">
      <PageHead title="Audit log">
        <Button variant="secondary" size="xs" className="ml-auto" onClick={() => void refetch()}>
          Refresh
        </Button>
      </PageHead>
      <DataTable
        columns={auditColumns}
        data={events}
        isLoading={isLoading}
        searchColumn="action"
        searchPlaceholder="Filter by action"
        getRowId={(event) => `${event.createdAt}:${event.action}:${event.targetId}`}
        emptyState={
          error ? (
            <EmptyState title="Failed to load audit log" description={error.message} />
          ) : (
            <EmptyState
              title="No audit events yet"
              description="Operator actions on tenants, accounts, and credentials will appear here."
            />
          )
        }
      />
    </div>
  );
}

export const auditTree = auditRoute;
