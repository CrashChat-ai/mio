export type Operator = {
  email: string;
  name: string;
  avatarUrl: string;
  role: "viewer" | "operator" | "credential-admin";
  expiresAt: string;
};

export type SessionResponse = {
  authenticated: boolean;
  authMode: string;
  operator?: Operator;
};

export type Tenant = {
  id: string;
  slug: string;
  displayName: string;
  status: string;
  createdAt: string;
  disabledAt?: string;
};

export type Account = {
  id: string;
  tenantId: string;
  channelType: string;
  provider: string;
  externalId: string;
  displayName: string;
  rateLimitPerSecond: number;
  rateLimitScope: string;
  createdAt: string;
  disabledAt?: string;
};

export type ChannelType = {
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

export type TailMessage = {
  id: string;
  tenantId: string;
  accountId: string;
  conversationId: string;
  channelType: string;
  senderDisplay: string;
  text: string;
  receivedAt: string;
};

export type CredentialMetadata = {
  accountId: string;
  hasCredential: boolean;
  authKind?: string;
  keyVersion?: number;
  expiresAt?: string;
  rotatedAt?: string;
};

export type WebhookInfo = {
  accountId: string;
  channelType: string;
  webhookUrl: string;
  routeAliases: string[];
  authKind: string;
  setupHint: string;
};

export type ConsumerHealth = {
  consumerName: string;
  stream: string;
  numPending: number;
  numAckPending: number;
  lastDelivered?: string;
};

export type LoadState = "idle" | "loading" | "ready" | "error";

export async function api<T>(url: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(url, { credentials: "same-origin", ...init });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export function jsonRequest(method: "POST" | "PATCH", body: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

export function roleAllows(actual: Operator["role"], required: Operator["role"]): boolean {
  return roleRank(actual) >= roleRank(required);
}

function roleRank(role: Operator["role"]): number {
  if (role === "credential-admin") return 3;
  if (role === "operator") return 2;
  return 1;
}

export function formatTime(value: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

export function formatDateTime(value: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

export function flagText(channel: ChannelType): string {
  const flags = [
    channel.supportsThreads ? "threads" : "",
    channel.supportsEdit ? "edit" : "",
    channel.supportsDelete ? "delete" : "",
  ].filter(Boolean);
  return flags.length > 0 ? flags.join(", ") : "read-only";
}
