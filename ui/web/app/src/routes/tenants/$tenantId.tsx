import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { accountsListQuery } from "../../queries/accounts";
import { tenantsListQuery } from "../../queries/tenants";
import { formatDateTime } from "../../lib/format";
import { accountColumns } from "../../components/data-table/columns/account-columns";
import { DataTable } from "../../components/data-table/data-table";
import { EmptyState } from "../../components/empty-state";
import { StatusBadge, statusVariant } from "../../components/status-badge";
import { PageHead } from "../../components/shell/page-head";
import { Button } from "../../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../../components/ui/card";
import { tenantsRoute } from "./tenants-route";

export const tenantDetailRoute = createRoute({
  getParentRoute: () => tenantsRoute,
  path: "$tenantId",
  staticData: { title: "Tenant detail" },
  loader: ({ context }) => context.queryClient.ensureQueryData(tenantsListQuery),
  component: TenantDetailPage,
});

function TenantDetailPage() {
  const { tenantId } = tenantDetailRoute.useParams();
  const navigate = useNavigate();
  const { data: tenants = [], isLoading } = useQuery(tenantsListQuery);
  const accountsQuery = useQuery(accountsListQuery(tenantId));
  const tenant = tenants.find((item) => item.id === tenantId);

  if (!isLoading && !tenant) {
    return (
      <EmptyState
        title="Tenant not found"
        description={tenantId}
        action={
          <Button asChild>
            <Link to="/tenants">Back to tenants</Link>
          </Button>
        }
      />
    );
  }

  return (
    <div className="grid gap-5">
      <PageHead title={tenant?.displayName || tenant?.slug || "Tenant"}>
        {tenant && (
          <StatusBadge variant={statusVariant(tenant.status)}>{tenant.status}</StatusBadge>
        )}
      </PageHead>
      <Card>
        <CardHeader>
          <CardTitle>Overview</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-8 gap-y-2 text-sm">
            <dt className="text-muted">ID</dt>
            <dd className="break-all font-mono text-xs tracking-[0.02em] text-fg-2">{tenant?.id}</dd>
            <dt className="text-muted">Slug</dt>
            <dd className="font-mono text-xs tracking-[0.02em] text-fg-2">{tenant?.slug}</dd>
            <dt className="text-muted">Display name</dt>
            <dd>{tenant?.displayName || "—"}</dd>
            <dt className="text-muted">Created</dt>
            <dd className="font-mono text-xs text-fg-2">
              {tenant ? formatDateTime(tenant.createdAt) : ""}
            </dd>
            {tenant?.disabledAt && (
              <>
                <dt className="text-muted">Disabled</dt>
                <dd className="font-mono text-xs text-danger">
                  {formatDateTime(tenant.disabledAt)}
                </dd>
              </>
            )}
          </dl>
        </CardContent>
      </Card>
      <section className="grid gap-3" aria-label="Tenant accounts">
        <div className="flex items-center justify-between">
          <h2 className="font-display text-base font-semibold leading-tight tracking-tight">
            Accounts
          </h2>
          <Button asChild variant="link" size="xs">
            <Link to="/accounts" search={{ tenant: tenantId }}>
              View all accounts
            </Link>
          </Button>
        </div>
        <DataTable
          columns={accountColumns}
          data={accountsQuery.data ?? []}
          isLoading={accountsQuery.isLoading}
          getRowId={(account) => account.id}
          onRowClick={(account) =>
            void navigate({
              to: "/accounts",
              search: { tenant: tenantId, panel: `account:${account.id}` },
            })
          }
          emptyState={
            accountsQuery.error ? (
              <EmptyState
                title="Failed to load accounts"
                description={accountsQuery.error.message}
              />
            ) : (
              <EmptyState
                title="No accounts yet"
                description="Accounts onboarded for this tenant will appear here."
              />
            )
          }
        />
      </section>
    </div>
  );
}
