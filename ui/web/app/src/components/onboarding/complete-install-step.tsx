import { Link } from "@tanstack/react-router";
import type { Account } from "../../lib/api/types";
import { useCompleteInstall } from "../../queries/installs";
import { Button } from "../ui/button";
import { toast } from "../ui/use-toast";

type CompleteInstallStepProps = {
  installId: string;
  account: Account | null;
  onAccount: (account: Account) => void;
};

export function CompleteInstallStep({ installId, account, onAccount }: CompleteInstallStepProps) {
  const complete = useCompleteInstall();

  function onComplete() {
    complete.mutate(installId, {
      onSuccess: (data) => {
        onAccount(data.account);
        toast({ title: "Install completed", description: data.account.displayName || data.account.id });
      },
      onError: (err) =>
        toast({ variant: "error", title: "Complete install failed", description: err.message }),
    });
  }

  if (account) {
    return (
      <div className="grid gap-4">
        <p className="text-sm text-fg-2">
          Account <span className="font-mono text-xs">{account.id}</span> is connected.
        </p>
        <Button asChild variant="primary" className="justify-self-start">
          <Link to="/accounts" search={{ tenant: account.tenantId, panel: `account:${account.id}` }}>
            Open account
          </Link>
        </Button>
      </div>
    );
  }

  return (
    <div>
      <Button
        type="button"
        variant="primary"
        disabled={complete.isPending || installId === ""}
        onClick={onComplete}
      >
        {complete.isPending ? "Completing…" : "Complete install"}
      </Button>
    </div>
  );
}
