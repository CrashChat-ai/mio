import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useRouterState } from "@tanstack/react-router";

const STORAGE_KEY = "mio.refetch.interval";
const OPTIONS = [5000, 10000, 30000, 60000] as const;
const DEFAULT_INTERVAL = 10000;

type RefetchContext = {
  interval: number;
  setInterval: (value: number) => void;
  frozen: boolean;
  setFrozen: (value: boolean) => void;
  suspended: boolean;
  setSelectionActive: (active: boolean) => void;
  refetchInterval: number | false;
};

const Ctx = createContext<RefetchContext | null>(null);

function readInterval(): number {
  const raw = Number(localStorage.getItem(STORAGE_KEY));
  return (OPTIONS as readonly number[]).includes(raw) ? raw : DEFAULT_INTERVAL;
}

export function RefetchIntervalProvider({ children }: { children: ReactNode }) {
  const [interval, setIntervalState] = useState(readInterval);
  const [frozen, setFrozen] = useState(false);
  const [selectionActive, setSelectionActive] = useState(false);
  const [hidden, setHidden] = useState(() => document.visibilityState === "hidden");
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const firstPath = useRef(pathname);

  useEffect(() => {
    if (pathname !== firstPath.current) {
      firstPath.current = pathname;
      setFrozen(false);
      setSelectionActive(false);
    }
  }, [pathname]);

  useEffect(() => {
    const onVisibility = () => setHidden(document.visibilityState === "hidden");
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);

  const setInterval = useCallback((value: number) => {
    setIntervalState(value);
    localStorage.setItem(STORAGE_KEY, String(value));
  }, []);

  const suspended = frozen || selectionActive || hidden;

  const value = useMemo<RefetchContext>(
    () => ({
      interval,
      setInterval,
      frozen,
      setFrozen,
      suspended,
      setSelectionActive,
      refetchInterval: suspended ? false : interval,
    }),
    [interval, setInterval, frozen, suspended],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useRefetchInterval(): RefetchContext {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useRefetchInterval outside RefetchIntervalProvider");
  return ctx;
}

export const REFETCH_OPTIONS = OPTIONS;
