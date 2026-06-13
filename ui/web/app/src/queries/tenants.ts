import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, jsonRequest } from "../lib/api/api";
import type { components } from "../lib/api/generated/schema";
import { queries } from "../lib/query-keys";

export const tenantsListQuery = queryOptions({
  ...queries.tenants.list,
  select: (data) => data.tenants,
});

export type CreateTenantInput = components["schemas"]["CreateTenantRequest"];

export function useCreateTenant() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTenantInput) =>
      api<components["schemas"]["TenantEnvelope"]>(
        "/api/admin/tenants",
        jsonRequest("POST", input),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queries.tenants._def }),
  });
}
