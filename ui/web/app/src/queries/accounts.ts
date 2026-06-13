import { queryOptions } from "@tanstack/react-query";
import { queries } from "../lib/query-keys";

export function accountsListQuery(tenantId: string) {
  return queryOptions({
    ...queries.accounts.list(tenantId),
    select: (data) => data.accounts,
  });
}

export function accountDetailQuery(accountId: string) {
  return queryOptions({
    ...queries.accounts.detail(accountId),
    select: (data) => data.account,
  });
}
