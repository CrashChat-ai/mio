import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, jsonRequest } from "../lib/api/api";
import type { components } from "../lib/api/generated/schema";
import { queries } from "../lib/query-keys";

type Schemas = components["schemas"];
export type StartInstallInput = Schemas["StartInstallRequest"];
export type StartInstallResult = Schemas["StartInstallResponse"];

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
      api<Schemas["AccountEnvelope"]>(
        "/api/admin/installs/complete",
        jsonRequest("POST", { installId }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queries.accounts._def }),
  });
}
