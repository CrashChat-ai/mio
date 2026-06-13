import { useState } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { authedRoute } from "./__root";
import { accountsListQuery } from "../queries/accounts";
import { tenantsListQuery } from "../queries/tenants";
import { PageHead } from "../components/shell/page-head";
import { LiveTailView } from "../components/tail/live-tail-view";
import { TailMessagePanel } from "../components/tail/tail-message-panel";
import { useSidePanel } from "../components/side-panel/use-side-panel";
import type { TailMessage } from "../lib/api/types";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../components/ui/select";

const tailSearchSchema = z.object({
  tenant: z.string().optional().catch(undefined),
  account: z.string().optional().catch(undefined),
});

export const tailRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/tail",
  staticData: { title: "Live tail" },
  validateSearch: (search) => tailSearchSchema.parse(search),
  loader: ({ context }) => void context.queryClient.prefetchQuery(tenantsListQuery),
  component: TailPage,
});

function TailPage() {
  const { tenant: tenantId, account: accountId } = tailRoute.useSearch();
  const navigate = useNavigate();
  const { data: tenants = [] } = useQuery(tenantsListQuery);
  const { data: accounts = [] } = useQuery({
    ...accountsListQuery(tenantId ?? ""),
    enabled: tenantId !== undefined && tenantId !== "",
  });
  const panel = useSidePanel();
  const [selected, setSelected] = useState<TailMessage | null>(null);

  return (
    <div className="grid gap-5">
      <PageHead title="Live tail">
        <div className="ml-auto flex items-center gap-2">
          <Select
            value={tenantId ?? ""}
            onValueChange={(value) => void navigate({ to: "/tail", search: { tenant: value } })}
          >
            <SelectTrigger aria-label="Tenant" className="w-48">
              <SelectValue placeholder="Tenant" />
            </SelectTrigger>
            <SelectContent>
              {tenants.map((tenant) => (
                <SelectItem key={tenant.id} value={tenant.id}>
                  {tenant.displayName || tenant.slug}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={accountId ?? ""}
            disabled={!tenantId}
            onValueChange={(value) =>
              void navigate({ to: "/tail", search: { tenant: tenantId, account: value } })
            }
          >
            <SelectTrigger aria-label="Account" className="w-56">
              <SelectValue placeholder="Account" />
            </SelectTrigger>
            <SelectContent>
              {accounts.map((account) => (
                <SelectItem key={account.id} value={account.id}>
                  {account.displayName || account.externalId}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </PageHead>
      <LiveTailView
        accountId={accountId}
        eyebrow={accountId ? "messages_inbound" : "live tail"}
        onRowClick={setSelected}
      />
      {selected && (
        <TailMessagePanel
          message={selected}
          width={panel.width}
          onClose={() => setSelected(null)}
          onResizeStart={panel.startResize}
        />
      )}
    </div>
  );
}

export const tailTree = tailRoute;
