import { ExternalLink } from "lucide-react";
import type { StartInstallResult } from "../../queries/installs";
import { useStartInstall } from "../../queries/installs";
import { Button } from "../ui/button";
import { toast } from "../ui/use-toast";
import { SecretCopier } from "./secret-copier";

type StartInstallStepProps = {
  tenantId: string;
  channelType: string;
  provider: string;
  result: StartInstallResult | null;
  onResult: (result: StartInstallResult) => void;
  onNext: () => void;
};

export function StartInstallStep({
  tenantId,
  channelType,
  provider,
  result,
  onResult,
  onNext,
}: StartInstallStepProps) {
  const startInstall = useStartInstall();

  function onStart() {
    startInstall.mutate(
      { tenantId, channelType, provider },
      {
        onSuccess: (data) => {
          onResult(data);
          if (data.oauthUrl) window.open(data.oauthUrl, "_blank", "noopener");
        },
        onError: (err) =>
          toast({ variant: "error", title: "Start install failed", description: err.message }),
      },
    );
  }

  return (
    <div className="grid gap-4">
      {!result ? (
        <Button
          type="button"
          variant="primary"
          disabled={startInstall.isPending}
          onClick={onStart}
        >
          {startInstall.isPending ? "Starting…" : "Start install"}
        </Button>
      ) : (
        <>
          <SecretCopier value={result.installId} label="Install ID" />
          {result.oauthUrl && (
            <div className="grid gap-2">
              <SecretCopier value={result.oauthUrl} label="OAuth authorize URL" />
              <Button asChild variant="secondary" size="xs" className="justify-self-start">
                <a href={result.oauthUrl} target="_blank" rel="noopener noreferrer">
                  <ExternalLink size={13} />
                  Open authorize page
                </a>
              </Button>
            </div>
          )}
          <Button type="button" variant="primary" onClick={onNext}>
            Continue
          </Button>
        </>
      )}
    </div>
  );
}
