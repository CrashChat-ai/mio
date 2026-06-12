import { createQueryKeys, mergeQueryKeys } from "@lukemorales/query-key-factory";
import { queryOptions } from "@tanstack/react-query";
import { api } from "./api/client";
import type {
  Account,
  ChannelType,
  ConsumerHealth,
  CredentialMetadata,
  SessionResponse,
  Tenant,
  WebhookInfo,
} from "./api/types";

const session = createQueryKeys("session", {
  current: {
    queryKey: null,
    queryFn: () => api<SessionResponse>("/api/session"),
  },
});

const tenants = createQueryKeys("tenants", {
  list: {
    queryKey: null,
    queryFn: () => api<{ tenants: Tenant[] }>("/api/admin/tenants"),
  },
});

const accounts = createQueryKeys("accounts", {
  list: (tenantId: string) => ({
    queryKey: [tenantId],
    queryFn: () =>
      api<{ accounts: Account[] }>(
        `/api/admin/accounts?tenant_id=${encodeURIComponent(tenantId)}`,
      ),
  }),
});

const channelTypes = createQueryKeys("channelTypes", {
  list: {
    queryKey: null,
    queryFn: () => api<{ channelTypes: ChannelType[] }>("/api/admin/channel-types"),
  },
});

const streamHealth = createQueryKeys("streamHealth", {
  consumers: {
    queryKey: null,
    queryFn: () => api<{ consumers: ConsumerHealth[] }>("/api/admin/stream-health"),
  },
});

const credential = createQueryKeys("credential", {
  metadata: (accountId: string) => ({
    queryKey: [accountId],
    queryFn: () =>
      api<{ credential: CredentialMetadata }>(
        `/api/admin/accounts/credential-metadata?account_id=${encodeURIComponent(accountId)}`,
      ),
  }),
});

const webhookInfo = createQueryKeys("webhookInfo", {
  detail: (accountId: string) => ({
    queryKey: [accountId],
    queryFn: () =>
      api<{ webhookInfo: WebhookInfo }>(
        `/api/admin/accounts/webhook-info?account_id=${encodeURIComponent(accountId)}`,
      ),
  }),
});

export const queries = mergeQueryKeys(
  session,
  tenants,
  accounts,
  channelTypes,
  streamHealth,
  credential,
  webhookInfo,
);

export const sessionQuery = queryOptions({
  ...queries.session.current,
  staleTime: 30_000,
});
