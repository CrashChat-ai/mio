import { useState } from "react";
import type { Account, WebhookInfo, ConsumerHealth, LoadState } from "./types";
import { api, roleAllows, formatDateTime } from "./types";
import type { Operator } from "./types";

type Props = {
  selectedAccount: Account | undefined;
  operatorRole: Operator["role"];
};

export function WebhookPanel({ selectedAccount, operatorRole }: Props) {
  const [webhookInfo, setWebhookInfo] = useState<WebhookInfo | null>(null);
  const [webhookState, setWebhookState] = useState<LoadState>("idle");
  const [health, setHealth] = useState<ConsumerHealth[]>([]);
  const [healthState, setHealthState] = useState<LoadState>("idle");
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");

  const canMutate = roleAllows(operatorRole, "operator");

  async function loadWebhookInfo() {
    if (!selectedAccount) return;
    setWebhookState("loading");
    setError("");
    try {
      const resp = await api<{ webhookInfo: WebhookInfo }>(
        `/api/admin/accounts/webhook-info?account_id=${encodeURIComponent(selectedAccount.id)}`,
      );
      setWebhookInfo(resp.webhookInfo);
      setWebhookState("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "webhook info failed");
      setWebhookState("error");
    }
  }

  async function loadStreamHealth() {
    setHealthState("loading");
    setError("");
    try {
      const resp = await api<{ consumers: ConsumerHealth[] }>("/api/admin/stream-health");
      setHealth(resp.consumers);
      setHealthState("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "stream health failed");
      setHealthState("error");
    }
  }

  async function copyURL(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setError("clipboard write failed");
    }
  }

  async function startInstallFlow() {
    if (!webhookInfo) return;
    const oauthUrl = webhookInfo.authKind === "oauth2_refresh"
      ? `/api/admin/installs/start`
      : null;
    if (!oauthUrl) return;
    window.open(oauthUrl, "_blank");
  }

  return (
    <section className="grid" id="onboarding">
      <section className="panel">
        <div className="panelHeader">
          <h2>Webhook setup</h2>
          <span className={`pill ${webhookState}`}>{webhookState}</span>
        </div>
        <div className="formStack">
          <div className="fieldGrid">
            <button type="button" onClick={() => void loadWebhookInfo()} disabled={!selectedAccount || webhookState === "loading"}>
              Load webhook info
            </button>
          </div>
          {error && <p className="errorText">{error}</p>}
          {webhookInfo && (
            <div className="credentialGrid">
              <span>Channel</span>
              <strong>{webhookInfo.channelType}</strong>
              <span>Auth kind</span>
              <strong>{webhookInfo.authKind}</strong>
              <span>Webhook URL</span>
              <span style={{ display: "flex", gap: "0.5rem", alignItems: "center", minWidth: 0 }}>
                <code style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {webhookInfo.webhookUrl || "(not configured — set MIO_PUBLIC_BASE_URL)"}
                </code>
                {webhookInfo.webhookUrl && (
                  <button type="button" onClick={() => void copyURL(webhookInfo.webhookUrl)}>
                    {copied ? "Copied" : "Copy"}
                  </button>
                )}
              </span>
              {webhookInfo.routeAliases.length > 0 && (
                <>
                  <span>Aliases</span>
                  <strong>{webhookInfo.routeAliases.join(", ")}</strong>
                </>
              )}
              <span>Next step</span>
              <span>{webhookInfo.setupHint}</span>
              {canMutate && webhookInfo.authKind === "oauth2_refresh" && (
                <>
                  <span />
                  <button type="button" onClick={() => void startInstallFlow()}>
                    Start OAuth install
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      </section>

      <section className="panel">
        <div className="panelHeader">
          <h2>Stream health</h2>
          <span className={`pill ${healthState}`}>{healthState}</span>
        </div>
        <div className="formStack">
          <div className="fieldGrid">
            <button type="button" onClick={() => void loadStreamHealth()} disabled={healthState === "loading"}>
              Refresh
            </button>
          </div>
          {health.length > 0 && (
            <div className="table">
              <div className="tableHead" style={{ gridTemplateColumns: "1.5fr 1.2fr 1fr 1fr 1.5fr" }}>
                <span>Consumer</span>
                <span>Stream</span>
                <span>Pending</span>
                <span>Ack pending</span>
                <span>Last delivered</span>
              </div>
              {health.map((c) => (
                <div className="tableRow" key={c.consumerName} style={{ gridTemplateColumns: "1.5fr 1.2fr 1fr 1fr 1.5fr" }}>
                  <span>{c.consumerName}</span>
                  <span>{c.stream}</span>
                  <span style={{ color: c.numPending > 0 ? "#b42318" : undefined }}>{c.numPending}</span>
                  <span>{c.numAckPending}</span>
                  <span>{c.lastDelivered ? formatDateTime(c.lastDelivered) : "—"}</span>
                </div>
              ))}
            </div>
          )}
          {healthState === "ready" && health.length === 0 && (
            <div className="emptyState">No consumers found</div>
          )}
        </div>
      </section>
    </section>
  );
}
