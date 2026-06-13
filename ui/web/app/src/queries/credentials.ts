import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, jsonRequest } from "../lib/api/client";
import { queries } from "../lib/query-keys";

export function credentialMetadataQuery(accountId: string) {
  return queryOptions({
    ...queries.credential.metadata(accountId),
    select: (data) => data.credential,
  });
}

export function useRotateCredential(accountId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      api<void>(
        "/api/admin/accounts/rotate-credential",
        jsonRequest("POST", { accountId }),
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: queries.credential.metadata(accountId).queryKey }),
  });
}
