import { createRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { authedRoute } from "../__root";
import { accountsListQuery } from "../../queries/accounts";
import { tenantsListQuery } from "../../queries/tenants";
import { accountColumns } from "../../components/data-table/columns/account-columns";
import { DataTable } from "../../components/data-table/data-table";
import { useZodColumnFilters } from "../../components/data-table/use-zod-column-filters";
import { EmptyState } from "../../components/empty-state";
import { useSidePanel } from "../../components/side-panel/use-side-panel";
import { PageHead } from "../../components/shell/page-head";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../../components/ui/select";
import { AccountPanel } from "./account-panel";

const accountsSearchSchema = z.object({
  tenant: z.string().optional().catch(undefined),
  panel: z.string().optional().catch(undefined),
  accountsFilter: z.string().optional().catch(undefined),
});

export const accountsRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/accounts",
  staticData: { title: "Accounts" },
  validateSearch: (search) => accountsSearchSchema.parse(search),
  loader: ({ context }) => void context.queryClient.prefetchQuery(tenantsListQuery),
  component: AccountsPage,
});

function AccountsPage() {
  const { tenant: tenantId } = accountsRoute.useSearch();
  const navigate = useNavigate();
  const { data: tenants = [] } = useQuery(tenantsListQuery);
  const accountsQuery = useQuery({
    ...accountsListQuery(tenantId ?? ""),
    enabled: tenantId !== undefined && tenantId !== "",
  });
  const accounts = accountsQuery.data ?? [];
  const [filters, setFilters] = useZodColumnFilters("accounts");
  const panel = useSidePanel();
  const entry = panel.entry;
  const activeAccount =
    entry?.kind === "account" ? accounts.find((account) => account.id === entry.id) : undefined;

  return (
    <div className="grid gap-5">
      <PageHead title="Accounts">
        <Select
          value={tenantId ?? ""}
          onValueChange={(value) => void navigate({ to: "/accounts", search: { tenant: value } })}
        >
          <SelectTrigger aria-label="Tenant" className="ml-auto w-56">
            <SelectValue placeholder="Select a tenant" />
          </SelectTrigger>
          <SelectContent>
            {tenants.map((tenant) => (
              <SelectItem key={tenant.id} value={tenant.id}>
                {tenant.displayName || tenant.slug}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </PageHead>
      {tenantId === undefined || tenantId === "" ? (
        <EmptyState
          title="Select a tenant"
          description="Accounts are scoped per tenant. Pick one to list its channel accounts."
        />
      ) : (
        <DataTable
          columns={accountColumns}
          data={accounts}
          isLoading={accountsQuery.isLoading}
          searchColumn="displayName"
          searchPlaceholder="Filter by name or external ID"
          columnFilters={filters}
          onColumnFiltersChange={setFilters}
          getRowId={(account) => account.id}
          activeRowId={entry?.kind === "account" ? entry.id : undefined}
          onRowClick={(account) => panel.open({ kind: "account", id: account.id })}
          emptyState={
            accountsQuery.error ? (
              <EmptyState
                title="Failed to load accounts"
                description={accountsQuery.error.message}
              />
            ) : (
              <EmptyState
                title="No accounts for this tenant"
                description="Run onboarding to install a channel account."
              />
            )
          }
        />
      )}
      {entry && <AccountPanel panel={panel} account={activeAccount} />}
    </div>
  );
}

export const accountsTree = accountsRoute;
