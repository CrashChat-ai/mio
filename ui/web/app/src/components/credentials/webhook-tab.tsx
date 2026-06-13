import { useQuery } from "@tanstack/react-query";
import type { WebhookInfo } from "../../lib/api/types";
import { webhookInfoQuery } from "../../queries/webhook-info";
import { CodeChip } from "../onboarding/code-chip";

export function WebhookTab({ accountId }: { accountId: string }) {
  const { data, isLoading, error } = useQuery(webhookInfoQuery(accountId));

  if (isLoading) return <p className="text-sm text-muted">Loading webhook info…</p>;
  if (error) return <p className="text-sm text-danger">{error.message}</p>;
  if (!data) return null;

  return <WebhookFacts info={data} />;
}

export function WebhookFacts({ info }: { info: WebhookInfo }) {
  return (
    <div className="grid gap-4">
      <div className="grid gap-1.5">
        <span className="eyebrow">Webhook URL</span>
        {info.webhookUrl ? (
          <CodeChip value={info.webhookUrl} label="Webhook URL" />
        ) : (
          <p className="text-sm text-muted">Not configured — set MIO_PUBLIC_BASE_URL.</p>
        )}
      </div>
      <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-6 gap-y-2 text-sm">
        <dt className="text-muted">Channel</dt>
        <dd className="text-fg-2">{info.channelType}</dd>
        <dt className="text-muted">Auth kind</dt>
        <dd className="text-fg-2">{info.authKind}</dd>
        {info.routeAliases.length > 0 && (
          <>
            <dt className="text-muted">Aliases</dt>
            <dd className="font-mono text-xs text-fg-2">{info.routeAliases.join(", ")}</dd>
          </>
        )}
        <dt className="text-muted">Next step</dt>
        <dd className="text-fg-2">{info.setupHint}</dd>
      </dl>
    </div>
  );
}
