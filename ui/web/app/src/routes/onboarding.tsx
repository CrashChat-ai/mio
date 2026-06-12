import { createRoute } from "@tanstack/react-router";
import { authedRoute } from "./__root";
import { PageHead } from "../components/shell/page-head";

export const onboardingRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/onboarding",
  staticData: { title: "Onboarding" },
  component: OnboardingPage,
});

function OnboardingPage() {
  return <PageHead title="Onboarding" />;
}

export const onboardingTree = onboardingRoute;
