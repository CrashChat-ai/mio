import { createRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { useSession } from "../../contexts/session-provider";
import { roleAllows } from "../../lib/roles";
import { tenantsListQuery } from "../../queries/tenants";
import { tenantColumns } from "../../components/data-table/columns/tenant-columns";
import { DataTable } from "../../components/data-table/data-table";
import { useZodColumnFilters } from "../../components/data-table/use-zod-column-filters";
import { EmptyState } from "../../components/empty-state";
import { PageHead } from "../../components/shell/page-head";
import { Badge } from "../../components/ui/badge";
import { CreateTenantDialog } from "./create-tenant-dialog";
import { tenantDetailRoute } from "./$tenantId";
import { tenantsRoute } from "./tenants-route";

const tenantsSearchSchema = z.object({
  tenantsFilter: z.string().optional().catch(undefined),
});

export const tenantsIndexRoute = createRoute({
  getParentRoute: () => tenantsRoute,
  path: "/",
  validateSearch: (search) => tenantsSearchSchema.parse(search),
  loader: ({ context }) => void context.queryClient.prefetchQuery(tenantsListQuery),
  component: TenantsPage,
});

function TenantsPage() {
  const navigate = useNavigate();
  const { role } = useSession();
  const { data: tenants = [], isLoading, error } = useQuery(tenantsListQuery);
  const [filters, setFilters] = useZodColumnFilters("tenants");

  return (
    <div className="grid gap-5">
      <PageHead title="Tenants">
        <Badge>{tenants.length}</Badge>
        <div className="ml-auto">
          <CreateTenantDialog disabled={!roleAllows(role, "operator")} />
        </div>
      </PageHead>
      <DataTable
        columns={tenantColumns}
        data={tenants}
        isLoading={isLoading}
        searchColumn="displayName"
        searchPlaceholder="Filter by name or slug"
        columnFilters={filters}
        onColumnFiltersChange={setFilters}
        getRowId={(tenant) => tenant.id}
        onRowClick={(tenant) =>
          void navigate({ to: "/tenants/$tenantId", params: { tenantId: tenant.id } })
        }
        emptyState={
          error ? (
            <EmptyState title="Failed to load tenants" description={error.message} />
          ) : (
            <EmptyState
              title="No tenants yet"
              description="Create the first tenant to start onboarding channel accounts."
            />
          )
        }
      />
    </div>
  );
}

export const tenantsTree = tenantsRoute.addChildren([tenantsIndexRoute, tenantDetailRoute]);
