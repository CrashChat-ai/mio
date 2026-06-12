import { useEffect, useMemo, useRef, useState } from "react";
import type {
  SessionResponse,
  Tenant,
  Account,
  ChannelType,
  TailMessage,
  CredentialMetadata,
  LoadState,
} from "./types";
import { api, jsonRequest, roleAllows, formatTime, formatDateTime } from "./types";
import { WebhookPanel } from "./WebhookPanel";

function App() {
  const [session, setSession] = useState<SessionResponse | null>(null);
  const [sessionState, setSessionState] = useState<LoadState>("loading");
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [channels, setChannels] = useState<ChannelType[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [selectedTenantID, setSelectedTenantID] = useState("");
  const [selectedAccountID, setSelectedAccountID] = useState("");
  const [conversationID, setConversationID] = useState("");
  const [messages, setMessages] = useState<TailMessage[]>([]);
  const [tailState, setTailState] = useState<"idle" | "connecting" | "open" | "closed">("idle");
  const [error, setError] = useState("");
  const [mutationState, setMutationState] = useState<LoadState>("idle");
  const [installForm, setInstallForm] = useState({ tenantId: "", channelType: "", provider: "default" });
  const [completeInstallID, setCompleteInstallID] = useState("");
  const [lastInstall, setLastInstall] = useState<{ installId: string; oauthUrl: string; redirectUri: string } | null>(null);
  const [accountForm, setAccountForm] = useState({
    displayName: "",
    externalId: "",
    rateLimitPerSecond: "0",
    rateLimitScope: "",
  });
  const [credential, setCredential] = useState<CredentialMetadata | null>(null);
  const tailRef = useRef<EventSource | null>(null);

  useEffect(() => {
    void loadSession();
  }, []);

  useEffect(() => {
    if (session?.authenticated) {
      void loadDashboard();
    }
  }, [session?.authenticated]);

  useEffect(() => {
    if (tenants.length > 0 && selectedTenantID === "") {
      setSelectedTenantID(tenants[0].id);
    }
  }, [tenants, selectedTenantID]);

  useEffect(() => {
    if (selectedTenantID !== "") {
      void loadAccounts(selectedTenantID);
      setInstallForm((current) => ({ ...current, tenantId: selectedTenantID }));
    } else {
      setAccounts([]);
      setSelectedAccountID("");
    }
  }, [selectedTenantID]);

  useEffect(() => {
    if (accounts.length > 0 && !accounts.some((account) => account.id === selectedAccountID)) {
      setSelectedAccountID(accounts[0].id);
    }
    if (accounts.length === 0) {
      setSelectedAccountID("");
    }
  }, [accounts, selectedAccountID]);

  useEffect(() => () => closeTail(), []);

  const selectedAccount = useMemo(
    () => accounts.find((account) => account.id === selectedAccountID),
    [accounts, selectedAccountID],
  );

  useEffect(() => {
    if (selectedAccount) {
      setAccountForm({
        displayName: selectedAccount.displayName,
        externalId: selectedAccount.externalId,
        rateLimitPerSecond: String(selectedAccount.rateLimitPerSecond || 0),
        rateLimitScope: selectedAccount.rateLimitScope,
      });
      setCredential(null);
    }
  }, [selectedAccount]);

  useEffect(() => {
    if (channels.length > 0 && installForm.channelType === "") {
      setInstallForm((current) => ({ ...current, channelType: channels[0].slug }));
    }
  }, [channels, installForm.channelType]);

  const operatorRole = session?.operator?.role ?? "viewer";
  const canMutate = roleAllows(operatorRole, "operator");
  const canRotateCredential = roleAllows(operatorRole, "credential-admin");

  async function loadSession() {
    setSessionState("loading");
    try {
      const next = await api<SessionResponse>("/api/session");
      setSession(next);
      setSessionState("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "session failed");
      setSessionState("error");
    }
  }

  async function loadDashboard() {
    setError("");
    try {
      const [tenantResponse, channelResponse] = await Promise.all([
        api<{ tenants: Tenant[] }>("/api/admin/tenants"),
        api<{ channelTypes: ChannelType[] }>("/api/admin/channel-types"),
      ]);
      setTenants(tenantResponse.tenants);
      setChannels(channelResponse.channelTypes);
    } catch (err) {
      setError(err instanceof Error ? err.message : "dashboard failed");
    }
  }

  async function loadAccounts(tenantID: string) {
    try {
      const response = await api<{ accounts: Account[] }>(
        `/api/admin/accounts?tenant_id=${encodeURIComponent(tenantID)}`,
      );
      setAccounts(response.accounts);
    } catch (err) {
      setError(err instanceof Error ? err.message : "accounts failed");
    }
  }

  async function runMutation(action: () => Promise<void>) {
    setMutationState("loading");
    setError("");
    try {
      await action();
      setMutationState("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "mutation failed");
      setMutationState("error");
    }
  }

  async function startInstall() {
    await runMutation(async () => {
      const response = await api<{ installId: string; oauthUrl: string; redirectUri: string }>(
        "/api/admin/installs/start",
        jsonRequest("POST", installForm),
      );
      setLastInstall(response);
      setCompleteInstallID(response.installId);
    });
  }

  async function completeInstall() {
    await runMutation(async () => {
      const response = await api<{ account: Account }>("/api/admin/installs/complete", jsonRequest("POST", {
        installId: completeInstallID,
      }));
      setSelectedAccountID(response.account.id);
      if (selectedTenantID) {
        await loadAccounts(selectedTenantID);
      }
    });
  }

  async function updateAccount() {
    if (!selectedAccountID) {
      return;
    }
    await runMutation(async () => {
      const response = await api<{ account: Account }>("/api/admin/accounts", jsonRequest("PATCH", {
        accountId: selectedAccountID,
        displayName: accountForm.displayName,
        externalId: accountForm.externalId,
      }));
      setSelectedAccountID(response.account.id);
      if (selectedTenantID) {
        await loadAccounts(selectedTenantID);
      }
    });
  }

  async function setRateLimit() {
    if (!selectedAccountID) {
      return;
    }
    await runMutation(async () => {
      await api<{ account: Account }>("/api/admin/accounts/rate-limit", jsonRequest("POST", {
        accountId: selectedAccountID,
        rateLimitPerSecond: Number(accountForm.rateLimitPerSecond || 0),
        rateLimitScope: accountForm.rateLimitScope,
      }));
      if (selectedTenantID) {
        await loadAccounts(selectedTenantID);
      }
    });
  }

  async function disableAccount() {
    if (!selectedAccountID) {
      return;
    }
    await runMutation(async () => {
      await api<void>("/api/admin/accounts/disable", jsonRequest("POST", { accountId: selectedAccountID }));
      if (selectedTenantID) {
        await loadAccounts(selectedTenantID);
      }
    });
  }

  async function loadCredentialMetadata() {
    if (!selectedAccountID) {
      return;
    }
    setError("");
    try {
      const response = await api<{ credential: CredentialMetadata }>(
        `/api/admin/accounts/credential-metadata?account_id=${encodeURIComponent(selectedAccountID)}`,
      );
      setCredential(response.credential);
    } catch (err) {
      setError(err instanceof Error ? err.message : "credential metadata failed");
    }
  }

  async function rotateCredential() {
    if (!selectedAccountID) {
      return;
    }
    await runMutation(async () => {
      await api<void>("/api/admin/accounts/rotate-credential", jsonRequest("POST", { accountId: selectedAccountID }));
      await loadCredentialMetadata();
    });
  }

  function openTail() {
    if (!selectedAccountID) {
      return;
    }
    closeTail();
    setMessages([]);
    setTailState("connecting");
    const params = new URLSearchParams({ account_id: selectedAccountID });
    if (conversationID.trim() !== "") {
      params.set("conversation_id", conversationID.trim());
    }
    const source = new EventSource(`/api/admin/messages/tail?${params.toString()}`);
    tailRef.current = source;
    source.addEventListener("open", () => setTailState("open"));
    source.addEventListener("message", (event) => {
      const msg = JSON.parse(event.data) as TailMessage;
      setMessages((current) => [msg, ...current].slice(0, 80));
      setTailState("open");
    });
    source.addEventListener("error", () => {
      setTailState("closed");
      source.close();
      if (tailRef.current === source) {
        tailRef.current = null;
      }
    });
  }

  function closeTail() {
    if (tailRef.current) {
      tailRef.current.close();
      tailRef.current = null;
    }
    setTailState((current) => (current === "idle" ? "idle" : "closed"));
  }

  async function logout() {
    closeTail();
    await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
    setSession({ authenticated: false, authMode: session?.authMode ?? "google" });
  }

  if (sessionState === "loading") {
    return <main className="centerStage">Loading operator session</main>;
  }

  if (!session?.authenticated) {
    return (
      <main className="loginPage">
        <section className="loginPanel">
          <div className="brandMark">MIO</div>
          <div>
            <p className="eyebrow">Operator Console</p>
            <h1>Admin workspace</h1>
          </div>
          <a className="primaryAction" href="/auth/login">
            Sign in
          </a>
          {error && <p className="errorText">{error}</p>}
        </section>
      </main>
    );
  }

  return (
    <main className="shell">
      <aside className="rail" aria-label="MIO admin sections">
        <div className="brandMark">MIO</div>
        <nav>
          <a href="#tenants" aria-current="page">Tenants</a>
          <a href="#accounts">Accounts</a>
          {canMutate && <a href="#manage">Manage</a>}
          <a href="#credentials">Credentials</a>
          <a href="#onboarding">Onboarding</a>
          <a href="#channels">Channels</a>
          <a href="#tail">Live tail</a>
        </nav>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <p className="eyebrow">Operator Console</p>
            <h1>Admin workspace</h1>
          </div>
          <div className="operatorStrip">
            <span>{session.operator?.email}</span>
            <span className="rolePill">{operatorRole}</span>
            <button type="button" onClick={() => void loadDashboard()}>Refresh</button>
            <button type="button" onClick={() => void logout()}>Sign out</button>
          </div>
        </header>

        {error && <div className="alert" role="alert">{error}</div>}

        <section className="summary" aria-label="Admin summary">
          <article>
            <span className="metric">{tenants.length}</span>
            <p>Tenants</p>
          </article>
          <article>
            <span className="metric">{accounts.length}</span>
            <p>Accounts</p>
          </article>
          <article>
            <span className="metric">{channels.length}</span>
            <p>Channel types</p>
          </article>
        </section>

        {canMutate && (
          <section className="grid" id="manage">
            <section className="panel">
              <div className="panelHeader">
                <h2>Channel install</h2>
                <span className={`pill ${mutationState}`}>{mutationState}</span>
              </div>
              <div className="formStack">
                <div className="fieldGrid">
                  <select
                    value={installForm.tenantId}
                    onChange={(event) => setInstallForm((current) => ({ ...current, tenantId: event.target.value }))}
                    aria-label="Install tenant"
                  >
                    {tenants.map((tenant) => (
                      <option key={tenant.id} value={tenant.id}>
                        {tenant.displayName || tenant.slug}
                      </option>
                    ))}
                  </select>
                  <select
                    value={installForm.channelType}
                    onChange={(event) => setInstallForm((current) => ({ ...current, channelType: event.target.value }))}
                    aria-label="Install channel type"
                  >
                    {channels.map((channel) => (
                      <option key={channel.slug} value={channel.slug}>
                        {channel.slug}
                      </option>
                    ))}
                  </select>
                  <input
                    value={installForm.provider}
                    onChange={(event) => setInstallForm((current) => ({ ...current, provider: event.target.value }))}
                    placeholder="provider"
                    aria-label="Install provider"
                  />
                  <button type="button" onClick={() => void startInstall()} disabled={mutationState === "loading" || !installForm.tenantId || !installForm.channelType}>
                    Start install
                  </button>
                </div>
                {lastInstall && (
                  <div className="installResult">
                    <strong>{lastInstall.installId}</strong>
                    <a href={lastInstall.oauthUrl || "#"} target="_blank" rel="noreferrer">Open OAuth</a>
                    <small>{lastInstall.redirectUri}</small>
                  </div>
                )}
                <div className="fieldGrid">
                  <input
                    value={completeInstallID}
                    onChange={(event) => setCompleteInstallID(event.target.value)}
                    placeholder="install_id"
                    aria-label="Install ID"
                  />
                  <button type="button" onClick={() => void completeInstall()} disabled={mutationState === "loading" || !completeInstallID.trim()}>
                    Complete install
                  </button>
                </div>
              </div>
            </section>

            <section className="panel">
              <div className="panelHeader">
                <h2>Account controls</h2>
                <span>{selectedAccount?.displayName || selectedAccount?.externalId || selectedAccount?.id || "none"}</span>
              </div>
              <div className="formStack">
                <div className="fieldGrid">
                  <input
                    value={accountForm.displayName}
                    onChange={(event) => setAccountForm((current) => ({ ...current, displayName: event.target.value }))}
                    placeholder="Display name"
                    aria-label="Account display name"
                  />
                  <input
                    value={accountForm.externalId}
                    onChange={(event) => setAccountForm((current) => ({ ...current, externalId: event.target.value }))}
                    placeholder="External ID"
                    aria-label="Account external ID"
                  />
                  <button type="button" onClick={() => void updateAccount()} disabled={mutationState === "loading" || !selectedAccount}>
                    Update account
                  </button>
                </div>
                <div className="fieldGrid">
                  <input
                    value={accountForm.rateLimitPerSecond}
                    onChange={(event) => setAccountForm((current) => ({ ...current, rateLimitPerSecond: event.target.value }))}
                    placeholder="0"
                    inputMode="numeric"
                    aria-label="Rate limit per second"
                  />
                  <input
                    value={accountForm.rateLimitScope}
                    onChange={(event) => setAccountForm((current) => ({ ...current, rateLimitScope: event.target.value }))}
                    placeholder="account"
                    aria-label="Rate limit scope"
                  />
                  <button type="button" onClick={() => void setRateLimit()} disabled={mutationState === "loading" || !selectedAccount}>
                    Set rate
                  </button>
                  <button type="button" className="dangerAction" onClick={() => void disableAccount()} disabled={mutationState === "loading" || !selectedAccount || Boolean(selectedAccount.disabledAt)}>
                    Disable
                  </button>
                </div>
              </div>
            </section>
          </section>
        )}

        <section className="grid">
          <section className="panel" id="credentials">
            <div className="panelHeader">
              <h2>Credentials</h2>
              <span>{credential?.hasCredential ? credential.authKind : "metadata"}</span>
            </div>
            <div className="credentialBody">
              <button type="button" onClick={() => void loadCredentialMetadata()} disabled={!selectedAccount}>
                Load metadata
              </button>
              <button type="button" onClick={() => void rotateCredential()} disabled={!selectedAccount || !canRotateCredential || mutationState === "loading"}>
                Rotate credential
              </button>
              {credential && (
                <div className="credentialGrid">
                  <span>Account</span>
                  <strong>{credential.accountId}</strong>
                  <span>Status</span>
                  <strong>{credential.hasCredential ? "stored" : "missing"}</strong>
                  <span>Key</span>
                  <strong>{credential.keyVersion || 0}</strong>
                  <span>Rotated</span>
                  <strong>{credential.rotatedAt ? formatDateTime(credential.rotatedAt) : ""}</strong>
                </div>
              )}
            </div>
          </section>
        </section>

        <WebhookPanel selectedAccount={selectedAccount} operatorRole={operatorRole} />

        <section className="grid">
          <section className="panel" id="tail">
            <div className="panelHeader">
              <h2>Live tail</h2>
              <span className={`pill ${tailState}`}>{tailState}</span>
            </div>
            <div className="tailControls">
              <select
                value={selectedTenantID}
                onChange={(event) => setSelectedTenantID(event.target.value)}
                aria-label="Tenant"
              >
                {tenants.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.displayName || tenant.slug}
                  </option>
                ))}
              </select>
              <select
                value={selectedAccountID}
                onChange={(event) => setSelectedAccountID(event.target.value)}
                aria-label="Account"
              >
                {accounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.displayName || account.externalId || account.id}
                  </option>
                ))}
              </select>
              <input
                value={conversationID}
                onChange={(event) => setConversationID(event.target.value)}
                placeholder="conversation_id"
                aria-label="Conversation ID"
              />
              <button type="button" onClick={openTail} disabled={!selectedAccount}>
                Connect
              </button>
              <button type="button" onClick={closeTail}>
                Stop
              </button>
            </div>
            <div className="messageList">
              {messages.map((message) => (
                <article key={`${message.id}-${message.receivedAt}`} className="messageItem">
                  <div>
                    <strong>{message.senderDisplay || message.channelType || "unknown"}</strong>
                    <time>{formatTime(message.receivedAt)}</time>
                  </div>
                  <p>{message.text || "(empty)"}</p>
                  <small>{message.conversationId}</small>
                </article>
              ))}
              {messages.length === 0 && <div className="emptyState">No messages</div>}
            </div>
          </section>
        </section>
      </section>
    </main>
  );
}

export default App;
