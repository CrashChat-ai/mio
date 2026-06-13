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

export type AuditEvent = {
  operatorEmail: string;
  operatorRole: string;
  action: string;
  targetType: string;
  targetId: string;
  result: string;
  error?: string;
  createdAt: string;
};

export type LoadState = "idle" | "loading" | "ready" | "error";
