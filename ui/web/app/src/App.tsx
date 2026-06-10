import { useEffect, useMemo, useRef, useState } from "react";

type Operator = {
  email: string;
  name: string;
  avatarUrl: string;
  expiresAt: string;
};

type SessionResponse = {
  authenticated: boolean;
  authMode: string;
  operator?: Operator;
};

type Tenant = {
  id: string;
  slug: string;
  displayName: string;
  status: string;
  createdAt: string;
  disabledAt?: string;
};

type Account = {
  id: string;
  tenantId: string;
  channelType: string;
  provider: string;
  externalId: string;
  displayName: string;
  createdAt: string;
  disabledAt?: string;
};

type ChannelType = {
  slug: string;
  status: string;
  authKind: string;
  supportsThreads: boolean;
  supportsEdit: boolean;
  supportsDelete: boolean;
  allowedAttachmentKinds: string[];
  rateLimitScope: string;
  rateLimitPerSecond: number;
  maxTextBytes: number;
};

type TailMessage = {
  id: string;
  tenantId: string;
  accountId: string;
  conversationId: string;
  channelType: string;
  senderDisplay: string;
  text: string;
  receivedAt: string;
};

type LoadState = "idle" | "loading" | "ready" | "error";

async function api<T>(url: string): Promise<T> {
  const response = await fetch(url, { credentials: "same-origin" });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return (await response.json()) as T;
}

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
  const [dataState, setDataState] = useState<LoadState>("idle");
  const [accountsState, setAccountsState] = useState<LoadState>("idle");
  const [tailState, setTailState] = useState<"idle" | "connecting" | "open" | "closed">("idle");
  const [error, setError] = useState("");
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

  const selectedTenant = useMemo(
    () => tenants.find((tenant) => tenant.id === selectedTenantID),
    [tenants, selectedTenantID],
  );
  const selectedAccount = useMemo(
    () => accounts.find((account) => account.id === selectedAccountID),
    [accounts, selectedAccountID],
  );

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
    setDataState("loading");
    setError("");
    try {
      const [tenantResponse, channelResponse] = await Promise.all([
        api<{ tenants: Tenant[] }>("/api/admin/tenants"),
        api<{ channelTypes: ChannelType[] }>("/api/admin/channel-types"),
      ]);
      setTenants(tenantResponse.tenants);
      setChannels(channelResponse.channelTypes);
      setDataState("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "dashboard failed");
      setDataState("error");
    }
  }

  async function loadAccounts(tenantID: string) {
    setAccountsState("loading");
    try {
      const response = await api<{ accounts: Account[] }>(
        `/api/admin/accounts?tenant_id=${encodeURIComponent(tenantID)}`,
      );
      setAccounts(response.accounts);
      setAccountsState("ready");
    } catch (err) {
      setError(err instanceof Error ? err.message : "accounts failed");
      setAccountsState("error");
    }
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

        <section className="grid">
          <section className="panel" id="tenants">
            <div className="panelHeader">
              <h2>Tenants</h2>
              <span className={`pill ${dataState}`}>{dataState}</span>
            </div>
            <div className="list">
              {tenants.map((tenant) => (
                <button
                  type="button"
                  className={`rowButton ${tenant.id === selectedTenantID ? "selected" : ""}`}
                  key={tenant.id}
                  onClick={() => setSelectedTenantID(tenant.id)}
                >
                  <span>
                    <strong>{tenant.displayName || tenant.slug}</strong>
                    <small>{tenant.slug}</small>
                  </span>
                  <span className="rowMeta">{tenant.status}</span>
                </button>
              ))}
            </div>
          </section>

          <section className="panel" id="accounts">
            <div className="panelHeader">
              <h2>Accounts</h2>
              <span className={`pill ${accountsState}`}>{accountsState}</span>
            </div>
            <div className="toolbar">
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
            </div>
            <div className="list compact">
              {accounts.map((account) => (
                <button
                  type="button"
                  className={`rowButton ${account.id === selectedAccountID ? "selected" : ""}`}
                  key={account.id}
                  onClick={() => setSelectedAccountID(account.id)}
                >
                  <span>
                    <strong>{account.displayName || account.externalId || account.id}</strong>
                    <small>{account.channelType} · {account.provider}</small>
                  </span>
                  <span className="rowMeta">{account.disabledAt ? "disabled" : "active"}</span>
                </button>
              ))}
            </div>
          </section>
        </section>

        <section className="grid">
          <section className="panel" id="channels">
            <div className="panelHeader">
              <h2>Channel types</h2>
              <span>{channels.length}</span>
            </div>
            <div className="table">
              <div className="tableHead">
                <span>Slug</span>
                <span>Auth</span>
                <span>Rate</span>
                <span>Flags</span>
              </div>
              {channels.map((channel) => (
                <div className="tableRow" key={channel.slug}>
                  <span>{channel.slug}</span>
                  <span>{channel.authKind || "none"}</span>
                  <span>{channel.rateLimitPerSecond || 0}/s {channel.rateLimitScope}</span>
                  <span>{flagText(channel)}</span>
                </div>
              ))}
            </div>
          </section>

          <section className="panel" id="tail">
            <div className="panelHeader">
              <h2>Live tail</h2>
              <span className={`pill ${tailState}`}>{tailState}</span>
            </div>
            <div className="tailControls">
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

function flagText(channel: ChannelType): string {
  const flags = [
    channel.supportsThreads ? "threads" : "",
    channel.supportsEdit ? "edit" : "",
    channel.supportsDelete ? "delete" : "",
  ].filter(Boolean);
  return flags.length > 0 ? flags.join(", ") : "read-only";
}

function formatTime(value: string): string {
  if (!value) {
    return "";
  }
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

export default App;
