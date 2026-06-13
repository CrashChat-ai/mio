import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import type { Account } from "../../lib/api/types";
import { accountDetailQuery } from "../../queries/accounts";
import { formatDateTime } from "../../lib/format";
import { accountStatus } from "../../components/data-table/columns/account-columns";
import { CredentialsTab } from "../../components/credentials/credentials-tab";
import { DisableAccountButton } from "../../components/credentials/disable-account-button";
import { WebhookTab } from "../../components/credentials/webhook-tab";
import { SidePanel } from "../../components/side-panel/side-panel";
import type { SidePanelState } from "../../components/side-panel/use-side-panel";
import { StatusBadge, statusVariant } from "../../components/status-badge";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../../components/ui/tabs";

export function AccountPanel({ panel, account }: { panel: SidePanelState; account?: Account }) {
  const entry = panel.entry;
  const detail = useQuery({
    ...accountDetailQuery(entry?.id ?? ""),
    enabled: entry?.kind === "account" && !account,
  });
  if (!entry) return null;
  const resolved = account ?? detail.data;

  return (
    <SidePanel
      title={
        entry.kind === "account"
          ? resolved?.displayName || resolved?.externalId || "Account"
          : "Tenant"
      }
      subtitle={entry.id}
      width={panel.width}
      canBack={panel.canBack}
      canForward={panel.canForward}
      onBack={panel.back}
      onForward={panel.forward}
      onClose={panel.close}
      onResizeStart={panel.startResize}
    >
      {entry.kind === "account" ? (
        resolved ? (
          <Tabs defaultValue="overview">
            <TabsList>
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="credentials">Credentials</TabsTrigger>
              <TabsTrigger value="webhook">Webhook</TabsTrigger>
            </TabsList>
            <TabsContent value="overview">
              <AccountOverview account={resolved} />
            </TabsContent>
            <TabsContent value="credentials">
              <CredentialsTab accountId={resolved.id} />
            </TabsContent>
            <TabsContent value="webhook">
              <WebhookTab accountId={resolved.id} />
            </TabsContent>
          </Tabs>
        ) : detail.isLoading ? (
          <p className="text-sm text-muted">Loading account…</p>
        ) : (
          <p className="text-sm text-muted">Account not found.</p>
        )
      ) : (
        <Button asChild variant="link">
          <Link to="/tenants/$tenantId" params={{ tenantId: entry.id }}>
            Open tenant detail
          </Link>
        </Button>
      )}
    </SidePanel>
  );
}

function AccountOverview({ account }: { account: Account }) {
  const status = accountStatus(account);
  return (
    <div className="grid gap-4">
      <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-6 gap-y-2 text-sm">
        <dt className="text-muted">Status</dt>
        <dd>
          <StatusBadge variant={statusVariant(status)}>{status}</StatusBadge>
        </dd>
        <dt className="text-muted">Channel</dt>
        <dd>
          <Badge>{account.channelType}</Badge>
        </dd>
        <dt className="text-muted">Provider</dt>
        <dd>{account.provider || "default"}</dd>
        <dt className="text-muted">External ID</dt>
        <dd className="break-all font-mono text-xs tracking-[0.02em] text-fg-2">
          {account.externalId || "—"}
        </dd>
        <dt className="text-muted">Rate limit</dt>
        <dd className="font-mono text-xs text-fg-2">
          {account.rateLimitPerSecond || 0}/s {account.rateLimitScope}
        </dd>
        <dt className="text-muted">Tenant</dt>
        <dd>
          <Button asChild variant="link" size="xs" className="px-0">
            <Link to="/tenants/$tenantId" params={{ tenantId: account.tenantId }}>
              <span className="font-mono text-xs tracking-[0.02em]">{account.tenantId}</span>
            </Link>
          </Button>
        </dd>
        <dt className="text-muted">Created</dt>
        <dd className="font-mono text-xs text-fg-2">{formatDateTime(account.createdAt)}</dd>
        {account.disabledAt && (
          <>
            <dt className="text-muted">Disabled</dt>
            <dd className="font-mono text-xs text-danger">{formatDateTime(account.disabledAt)}</dd>
          </>
        )}
      </dl>
      {!account.disabledAt && (
        <div className="border-t border-border-soft pt-3">
          <DisableAccountButton
            accountId={account.id}
            label={account.displayName || account.externalId || account.id}
          />
        </div>
      )}
    </div>
  );
}
