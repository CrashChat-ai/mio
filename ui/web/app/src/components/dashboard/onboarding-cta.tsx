import { Link } from "@tanstack/react-router";
import { Card } from "../ui/card";
import { buttonVariants } from "../ui/button";

const STEPS = ["Create tenant", "Start install", "Paste webhook"];

export function OnboardingCta() {
  return (
    <Card aria-label="Connect a channel" className="grid content-start gap-4 px-5 py-5">
      <div>
        <p className="eyebrow">onboarding</p>
        <h2 className="font-display text-base font-semibold leading-tight">Connect a channel</h2>
      </div>
      <p className="text-sm text-muted">
        Run the guided install to point a Zoho Cliq or Telegram channel at the gateway and start
        ingesting messages.
      </p>
      <ol className="m-0 flex flex-wrap gap-x-4 gap-y-2 p-0 text-xs text-muted">
        {STEPS.map((step, index) => (
          <li key={step} className="inline-flex items-baseline gap-1">
            <span className="font-mono font-semibold text-fg-2">{index + 1}</span>
            {step}
          </li>
        ))}
      </ol>
      <div className="flex">
        <Link to="/onboarding" className={buttonVariants({ variant: "primary" })}>
          Open onboarding
        </Link>
      </div>
    </Card>
  );
}
