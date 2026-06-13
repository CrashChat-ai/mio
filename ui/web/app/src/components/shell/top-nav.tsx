import { Link, useNavigate } from "@tanstack/react-router";
import { Monitor, Moon, Sun } from "lucide-react";
import { apiUrl } from "../../lib/api/config";
import { useSession } from "../../contexts/session-provider";
import { useTheme } from "../../contexts/theme-provider";
import { queryClient } from "../../lib/query-client";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { useBreadcrumbs } from "./use-breadcrumbs";

const THEME_CYCLE = { dark: "light", light: "system", system: "dark" } as const;
const THEME_ICONS = { dark: Moon, light: Sun, system: Monitor } as const;

export function TopNav() {
  const { session, operator, role } = useSession();
  const crumbs = useBreadcrumbs();
  const navigate = useNavigate();
  const { theme, setTheme } = useTheme();
  const ThemeIcon = THEME_ICONS[theme];

  async function signOut() {
    await fetch(apiUrl("/auth/logout"), { method: "POST", credentials: "same-origin" });
    queryClient.removeQueries();
    await navigate({ to: "/login" });
  }

  return (
    <header className="sticky top-0 z-10 border-b border-border bg-bg/85 backdrop-blur-sm">
      <div className="mx-auto flex min-h-[52px] w-full max-w-[1280px] items-center gap-4 px-7 max-md:px-5">
        {crumbs.length > 1 ? (
          <nav aria-label="Breadcrumb">
            <ol className="flex items-center gap-2 text-sm">
              {crumbs.slice(0, -1).map((crumb) => (
                <li key={crumb.path} className="flex items-center gap-2">
                  <Link to={crumb.path} className="text-muted transition-colors hover:text-fg">
                    {crumb.title}
                  </Link>
                  <span aria-hidden="true" className="text-fg-faint">
                    /
                  </span>
                </li>
              ))}
              <li>
                <strong className="font-medium text-fg">{crumbs[crumbs.length - 1].title}</strong>
              </li>
            </ol>
          </nav>
        ) : (
          <span className="inline-flex min-h-[22px] items-center gap-2 rounded-sm border border-border bg-surface px-2 py-1 font-mono text-xs text-fg-2">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-muted" />
            mio · {session.authMode}
          </span>
        )}
        <div className="relative ml-auto w-60 max-md:hidden">
          <Input type="search" placeholder="Search" aria-label="Search console" className="pr-12" />
          <kbd
            aria-hidden="true"
            className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm border border-border bg-surface-2 px-2 py-1 font-mono text-xs text-muted"
          >
            ⌘K
          </kbd>
        </div>
        <Badge className="whitespace-nowrap max-lg:hidden">
          {operator?.email ?? "anonymous"} · {role}
        </Badge>
        <Button variant="ghost" size="icon" aria-label={`Theme: ${theme}`} onClick={() => setTheme(THEME_CYCLE[theme])}>
          <ThemeIcon size={16} />
        </Button>
        <Button variant="ghost" onClick={() => void signOut()}>
          Sign out
        </Button>
      </div>
    </header>
  );
}
