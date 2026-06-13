import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { ThemeProvider } from "./contexts/theme-provider";
import { setUnauthorizedHandler } from "./lib/api/api";
import { queryClient } from "./lib/query-client";
import { queries } from "./lib/query-keys";
import { router } from "./router";
import { TooltipProvider } from "./components/ui/tooltip";
import "./styles/theme.css";

setUnauthorizedHandler(() => {
  queryClient.removeQueries({ queryKey: queries.session.current.queryKey });
  void router.navigate({ to: "/login" });
});

const root = document.getElementById("root");

if (!root) {
  throw new Error("missing root element");
}

createRoot(root).render(
  <StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider delayDuration={200}>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
);
