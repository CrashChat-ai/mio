import { queryOptions } from "@tanstack/react-query";
import { queries } from "../lib/query-keys";

export function tenantDetailQuery(tenantId: string) {
  return queryOptions({
    ...queries.tenants.detail(tenantId),
    select: (data) => data.tenant,
  });
}
