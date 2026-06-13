import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { CredentialMetadata } from "../../lib/api/types";
import { formatDateTime } from "../../lib/format";
import { useSession } from "../../contexts/session-provider";
import { roleAllows } from "../../lib/roles";
import { credentialMetadataQuery, useRotateCredential } from "../../queries/credentials";
import { Button } from "../ui/button";
import { StatusBadge } from "../status-badge";
import { toast } from "../ui/use-toast";
import { ConfirmActionModal } from "./confirm-action-modal";

export function CredentialsTab({ accountId }: { accountId: string }) {
  const { role } = useSession();
  const canRotate = roleAllows(role, "credential-admin");
  const { data, isLoading, error } = useQuery(credentialMetadataQuery(accountId));
  const rotate = useRotateCredential(accountId);
  const [confirming, setConfirming] = useState(false);

  if (isLoading) return <p className="text-sm text-muted">Loading credential metadata…</p>;
  if (error) return <p className="text-sm text-danger">{error.message}</p>;
  if (!data) return null;

  function onRotate() {
    rotate.mutate(undefined, {
      onSuccess: () => {
        toast({ title: "Credential rotated", description: "New key version provisioned." });
        setConfirming(false);
      },
      onError: (err) =>
        toast({ variant: "error", title: "Rotate failed", description: err.message }),
    });
  }

  return (
    <div className="grid gap-4">
      <CredentialFacts meta={data} />
      <div className="flex items-center gap-3">
        <Button
          type="button"
          variant="primary"
          size="xs"
          disabled={!canRotate}
          title={canRotate ? undefined : "Requires credential-admin role"}
          onClick={() => setConfirming(true)}
        >
          Rotate credential
        </Button>
        {!canRotate && (
          <span className="text-xs text-muted">Rotation requires credential-admin.</span>
        )}
      </div>
      <ConfirmActionModal
        open={confirming}
        onOpenChange={setConfirming}
        title="Rotate credential"
        description="Provisions a new key version and invalidates the current secret. The plaintext secret is never shown here."
        confirmLabel="Rotate"
        pendingLabel="Rotating…"
        pending={rotate.isPending}
        onConfirm={onRotate}
      />
    </div>
  );
}

function CredentialFacts({ meta }: { meta: CredentialMetadata }) {
  return (
    <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-6 gap-y-2 text-sm">
      <dt className="text-muted">Credential</dt>
      <dd>
        <StatusBadge variant={meta.hasCredential ? "ok" : "danger"}>
          {meta.hasCredential ? "present" : "missing"}
        </StatusBadge>
      </dd>
      <dt className="text-muted">Auth kind</dt>
      <dd className="text-fg-2">{meta.authKind || "—"}</dd>
      <dt className="text-muted">Key version</dt>
      <dd className="font-mono text-xs text-fg-2">{meta.keyVersion ?? "—"}</dd>
      <dt className="text-muted">Rotated</dt>
      <dd className="font-mono text-xs text-fg-2">
        {meta.rotatedAt ? formatDateTime(meta.rotatedAt) : "—"}
      </dd>
      <dt className="text-muted">Expires</dt>
      <dd className="font-mono text-xs text-fg-2">
        {meta.expiresAt ? formatDateTime(meta.expiresAt) : "—"}
      </dd>
    </dl>
  );
}
