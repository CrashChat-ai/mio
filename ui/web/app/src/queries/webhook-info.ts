import { queryOptions } from "@tanstack/react-query";
import { queries } from "../lib/query-keys";

export function webhookInfoQuery(accountId: string) {
  return queryOptions({
    ...queries.webhookInfo.detail(accountId),
    select: (data) => data.webhookInfo,
  });
}
