import { createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./__root";
import { sessionQuery } from "../lib/query-keys";
import { apiUrl } from "../lib/api/config";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  staticData: { title: "Sign in" },
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQuery);
    if (session.authenticated) {
      throw redirect({ to: "/dashboard" });
    }
    return { authMode: session.authMode };
  },
  component: LoginPage,
});

function LoginPage() {
  const { authMode } = loginRoute.useRouteContext();

  return (
    <main className="grid min-h-dvh place-items-center bg-bg px-4">
      <Card className="w-full max-w-sm">
        <CardContent className="flex flex-col gap-5 p-8">
          <div className="flex items-center gap-3">
            <span
              aria-hidden="true"
              className="grid size-8 place-items-center rounded-sm bg-accent text-accent-on"
            >
              <svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2.4">
                <path d="M2.5 14V2.5L8 9l5.5-6.5V14" />
              </svg>
            </span>
            <span className="leading-tight">
              <span className="block font-display text-base font-semibold tracking-wide">MIO</span>
              <span className="block font-mono text-xs text-muted">operator console</span>
            </span>
          </div>
          <div className="grid gap-1">
            <p className="eyebrow">sign in</p>
            <h1 className="font-display text-xl font-semibold leading-tight tracking-[-0.015em]">
              Operator workspace
            </h1>
          </div>
          <p className="text-sm text-muted">
            {authMode === "dev"
              ? "Local development mode — a dev operator session is issued on sign-in."
              : "Sign in with your Google operator account."}
          </p>
          <Button variant="primary" asChild>
            <a href={apiUrl("/auth/login")}>{authMode === "dev" ? "Continue as dev operator" : "Continue with Google"}</a>
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}
