import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api/api";

const RETRYABLE_STATUSES = new Set([408, 429]);

function shouldRetry(failureCount: number, error: unknown): boolean {
  if (
    error instanceof ApiError &&
    error.status >= 400 &&
    error.status < 500 &&
    !RETRYABLE_STATUSES.has(error.status)
  ) {
    return false;
  }
  return failureCount < 2;
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: shouldRetry,
      refetchOnWindowFocus: false,
      staleTime: 10_000,
    },
  },
});
