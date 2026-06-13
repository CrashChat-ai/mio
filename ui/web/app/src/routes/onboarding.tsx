import { useMemo, useState } from "react";
import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import type { Account } from "../lib/api/types";
import type { StartInstallResult } from "../queries/installs";
import { channelTypesListQuery } from "../queries/channel-types";
import { tenantsListQuery } from "../queries/tenants";
import { PageHead } from "../components/shell/page-head";
import { Card, CardContent } from "../components/ui/card";
import { Steps, type StepDefinition } from "../components/onboarding/steps";
import { useSteps } from "../components/onboarding/use-steps";
import { ChannelTypeStep } from "../components/onboarding/channel-type-step";
import { StartInstallStep } from "../components/onboarding/start-install-step";
import { WebhookSetupStep } from "../components/onboarding/webhook-setup-step";
import { CompleteInstallStep } from "../components/onboarding/complete-install-step";

export const onboardingRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/onboarding",
  staticData: { title: "Onboarding" },
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(tenantsListQuery);
    void context.queryClient.prefetchQuery(channelTypesListQuery);
  },
  component: OnboardingPage,
});

const PROVIDER = "default";

function OnboardingPage() {
  const [tenantId, setTenantId] = useState("");
  const [channelType, setChannelType] = useState("");
  const [install, setInstall] = useState<StartInstallResult | null>(null);
  const [account, setAccount] = useState<Account | null>(null);
  const steps = useSteps(4);

  const definitions = useMemo<StepDefinition[]>(
    () => [
      {
        title: "Choose channel",
        summary: tenantId && channelType ? `${channelType} · ${tenantId}` : undefined,
        content: (
          <ChannelTypeStep
            tenantId={tenantId}
            channelType={channelType}
            onTenantChange={setTenantId}
            onChannelTypeChange={setChannelType}
            onNext={steps.advance}
          />
        ),
      },
      {
        title: "Start install",
        summary: install?.installId,
        content: (
          <StartInstallStep
            tenantId={tenantId}
            channelType={channelType}
            provider={PROVIDER}
            result={install}
            onResult={setInstall}
            onNext={steps.advance}
          />
        ),
      },
      {
        title: "Set up webhook",
        content: <WebhookSetupStep channelType={channelType} onNext={steps.advance} />,
      },
      {
        title: "Complete install",
        summary: account?.id,
        content: (
          <CompleteInstallStep
            installId={install?.installId ?? ""}
            account={account}
            onAccount={setAccount}
          />
        ),
      },
    ],
    [tenantId, channelType, install, account, steps.advance],
  );

  return (
    <div className="grid gap-5">
      <PageHead title="Onboarding" />
      <Card className="max-w-2xl">
        <CardContent className="grid gap-2">
          <p className="text-sm text-muted">
            Connect a channel: choose channel, start install, paste the webhook, then complete.
          </p>
          <Steps steps={definitions} statusOf={steps.statusOf} />
        </CardContent>
      </Card>
    </div>
  );
}

export const onboardingTree = onboardingRoute;
