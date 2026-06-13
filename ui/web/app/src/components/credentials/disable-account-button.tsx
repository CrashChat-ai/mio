import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, jsonRequest } from "../../lib/api/api";
import { queries } from "../../lib/query-keys";
import { useSession } from "../../contexts/session-provider";
import { roleAllows } from "../../lib/roles";
import { Button } from "../ui/button";
import { toast } from "../ui/use-toast";
import { ConfirmActionModal } from "./confirm-action-modal";

export function DisableAccountButton({
  accountId,
  label,
}: {
  accountId: string;
  label: string;
}) {
  const { role } = useSession();
  const canDisable = roleAllows(role, "operator");
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const disable = useMutation({
    mutationFn: () =>
      api<void>("/api/admin/accounts/disable", jsonRequest("POST", { accountId })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queries.accounts._def }),
  });

  function onDisable() {
    disable.mutate(undefined, {
      onSuccess: () => {
        toast({ title: "Account disabled", description: label });
        setConfirming(false);
      },
      onError: (err) =>
        toast({ variant: "error", title: "Disable failed", description: err.message }),
    });
  }

  return (
    <>
      <Button
        type="button"
        variant="danger"
        size="xs"
        disabled={!canDisable}
        title={canDisable ? undefined : "Requires operator role"}
        onClick={() => setConfirming(true)}
      >
        Disable account
      </Button>
      <ConfirmActionModal
        open={confirming}
        onOpenChange={setConfirming}
        title="Disable account"
        description={`Stops ingestion for ${label}. The account can be re-enabled later.`}
        confirmLabel="Disable"
        pendingLabel="Disabling…"
        destructive
        pending={disable.isPending}
        onConfirm={onDisable}
      />
    </>
  );
}
