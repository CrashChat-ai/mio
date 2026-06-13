import { Outlet, createRootRouteWithContext, createRoute, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { SessionProvider } from "../contexts/session-provider";
import { sessionQuery } from "../lib/query-keys";
import { AppFooter } from "../components/shell/app-footer";
import { SideNav } from "../components/shell/side-nav";
import { TopNav } from "../components/shell/top-nav";
import { COLLAPSED_WIDTH, useNavState } from "../components/shell/use-nav-state";
import { Toaster } from "../components/ui/toaster";

export type RouterContext = {
  queryClient: QueryClient;
};

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
});

function RootLayout() {
  return (
    <>
      <Outlet />
      <Toaster />
    </>
  );
}

export const authedRoute = createRoute({
  id: "authed",
  getParentRoute: () => rootRoute,
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQuery);
    if (!session.authenticated) {
      throw redirect({ to: "/login" });
    }
    return { session };
  },
  component: AuthedLayout,
});

function AuthedLayout() {
  const { session } = authedRoute.useRouteContext();
  const nav = useNavState();

  return (
    <SessionProvider session={session}>
      <div
        className="grid min-h-dvh bg-bg transition-[grid-template-columns] duration-200"
        style={{
          gridTemplateColumns: `${nav.collapsed ? COLLAPSED_WIDTH : nav.width}px minmax(0, 1fr)`,
        }}
      >
        <SideNav collapsed={nav.collapsed} onToggle={nav.toggle} onResizeStart={nav.startResize} />
        <div className="flex min-w-0 flex-col">
          <TopNav />
          <main className="mx-auto w-full max-w-[1280px] flex-1 px-7 py-6 max-md:px-5">
            <Outlet />
          </main>
          <AppFooter />
        </div>
      </div>
    </SessionProvider>
  );
}
