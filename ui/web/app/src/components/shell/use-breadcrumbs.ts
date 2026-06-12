import { useMatches } from "@tanstack/react-router";

export type Crumb = { title: string; path: string };

export function useBreadcrumbs(): Crumb[] {
  const matches = useMatches();
  return matches.flatMap((match) => {
    const title = match.staticData.title;
    return title ? [{ title, path: match.pathname }] : [];
  });
}
