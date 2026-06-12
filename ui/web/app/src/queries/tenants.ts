import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, jsonRequest } from "../lib/api/client";
import type { Tenant } from "../lib/api/types";
import { queries } from "../lib/query-keys";

export const tenantsListQuery = queryOptions({
  ...queries.tenants.list,
  select: (data) => data.tenants,
});

export type CreateTenantInput = { slug: string; displayName: string };

export function useCreateTenant() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTenantInput) =>
      api<{ tenant: Tenant }>("/api/admin/tenants", jsonRequest("POST", input)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queries.tenants._def }),
  });
}
