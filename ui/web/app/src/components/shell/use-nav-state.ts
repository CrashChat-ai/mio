import { useCallback, useEffect, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";

const STORAGE_KEY = "mio.nav";
const MIN_WIDTH = 180;
const MAX_WIDTH = 320;
const SNAP_WIDTH = 150;
const DEFAULT_WIDTH = 224;
export const COLLAPSED_WIDTH = 60;

type NavState = { width: number; collapsed: boolean };

function readStored(): NavState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<NavState>;
      return {
        width: clampWidth(typeof parsed.width === "number" ? parsed.width : DEFAULT_WIDTH),
        collapsed: parsed.collapsed === true,
      };
    }
  } catch {
    localStorage.removeItem(STORAGE_KEY);
  }
  return { width: DEFAULT_WIDTH, collapsed: false };
}

function clampWidth(width: number): number {
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, width));
}

export function useNavState() {
  const [state, setState] = useState<NavState>(readStored);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  }, [state]);

  useEffect(() => {
    const media = window.matchMedia("(max-width: 860px)");
    const sync = () => {
      if (media.matches) {
        setState((current) => ({ ...current, collapsed: true }));
      }
    };
    sync();
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, []);

  const toggle = useCallback(() => {
    setState((current) => ({ ...current, collapsed: !current.collapsed }));
  }, []);

  const startResize = useCallback((event: ReactPointerEvent) => {
    event.preventDefault();
    const onMove = (move: PointerEvent) => {
      if (move.clientX < SNAP_WIDTH) {
        setState((current) => ({ ...current, collapsed: true }));
        return;
      }
      setState({ width: clampWidth(move.clientX), collapsed: false });
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }, []);

  return { ...state, toggle, startResize };
}
