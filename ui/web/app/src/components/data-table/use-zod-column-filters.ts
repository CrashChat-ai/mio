import { useCallback, useMemo } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import type { ColumnFiltersState, Updater } from "@tanstack/react-table";
import { z } from "zod";

const filtersSchema = z.array(z.object({ id: z.string(), value: z.unknown() }));

function parseFilters(raw: unknown): ColumnFiltersState {
  if (typeof raw !== "string" || raw === "") return [];
  try {
    const result = filtersSchema.safeParse(JSON.parse(raw));
    return result.success ? (result.data as ColumnFiltersState) : [];
  } catch {
    return [];
  }
}

export function useZodColumnFilters(tableId: string) {
  const paramKey = `${tableId}Filter`;
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const navigate = useNavigate();
  const raw = search[paramKey];

  const filters = useMemo(() => parseFilters(raw), [raw]);

  const setFilters = useCallback(
    (updater: Updater<ColumnFiltersState>) => {
      const next = typeof updater === "function" ? updater(parseFilters(raw)) : updater;
      void navigate({
        to: ".",
        search: (prev: Record<string, unknown>) => ({
          ...prev,
          [paramKey]: next.length > 0 ? JSON.stringify(next) : undefined,
        }),
        replace: true,
      });
    },
    [navigate, paramKey, raw],
  );

  return [filters, setFilters] as const;
}
