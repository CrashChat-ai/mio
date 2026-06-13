import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, jsonRequest } from "../lib/api/client";
import type { Account } from "../lib/api/types";
import { queries } from "../lib/query-keys";

export type StartInstallInput = { tenantId: string; channelType: string; provider: string };
export type StartInstallResult = { installId: string; oauthUrl: string; redirectUri: string };

export function useStartInstall() {
  return useMutation({
    mutationFn: (input: StartInstallInput) =>
      api<StartInstallResult>("/api/admin/installs/start", jsonRequest("POST", input)),
  });
}

export function useCompleteInstall() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (installId: string) =>
      api<{ account: Account }>("/api/admin/installs/complete", jsonRequest("POST", { installId })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queries.accounts._def }),
  });
}
