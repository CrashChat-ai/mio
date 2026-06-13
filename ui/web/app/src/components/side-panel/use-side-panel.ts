import { useCallback, useEffect, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";

const STORAGE_KEY = "mio.sidepanel.width";
const MIN_WIDTH = 320;
const MAX_WIDTH = 720;
const DEFAULT_WIDTH = 420;

export type PanelEntry = { kind: "tenant" | "account"; id: string };

export function parsePanelParam(raw: unknown): PanelEntry | null {
  if (typeof raw !== "string") return null;
  const separator = raw.indexOf(":");
  if (separator <= 0) return null;
  const kind = raw.slice(0, separator);
  const id = raw.slice(separator + 1);
  if ((kind === "tenant" || kind === "account") && id !== "") return { kind, id };
  return null;
}

export function serializePanel(entry: PanelEntry): string {
  return `${entry.kind}:${entry.id}`;
}

function sameEntry(a: PanelEntry | undefined, b: PanelEntry): boolean {
  return a !== undefined && a.kind === b.kind && a.id === b.id;
}

function clampWidth(width: number): number {
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, width));
}

function readWidth(): number {
  const raw = Number(localStorage.getItem(STORAGE_KEY));
  return Number.isFinite(raw) && raw > 0 ? clampWidth(raw) : DEFAULT_WIDTH;
}

type PanelHistory = { stack: PanelEntry[]; index: number };

export function useSidePanel() {
  const search = useSearch({ strict: false }) as { panel?: string };
  const navigate = useNavigate();
  const entry = parsePanelParam(search.panel);
  const [history, setHistory] = useState<PanelHistory>({ stack: [], index: -1 });
  const [width, setWidth] = useState(readWidth);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, String(width));
  }, [width]);

  useEffect(() => {
    setHistory((current) => {
      if (!entry) return current.stack.length === 0 ? current : { stack: [], index: -1 };
      if (sameEntry(current.stack[current.index], entry)) return current;
      const existing = current.stack.findIndex((item) => sameEntry(item, entry));
      if (existing >= 0) return { ...current, index: existing };
      const stack = [...current.stack.slice(0, current.index + 1), entry];
      return { stack, index: stack.length - 1 };
    });
  }, [search.panel]);

  const setPanelParam = useCallback(
    (value: string | undefined, replace: boolean) => {
      void navigate({
        to: ".",
        search: (prev: Record<string, unknown>) => ({ ...prev, panel: value }),
        replace,
      });
    },
    [navigate],
  );

  const open = useCallback(
    (next: PanelEntry) => setPanelParam(serializePanel(next), false),
    [setPanelParam],
  );

  const close = useCallback(() => setPanelParam(undefined, false), [setPanelParam]);

  const back = useCallback(() => {
    if (history.index <= 0) return;
    const target = history.stack[history.index - 1];
    setHistory({ ...history, index: history.index - 1 });
    setPanelParam(serializePanel(target), true);
  }, [history, setPanelParam]);

  const forward = useCallback(() => {
    if (history.index >= history.stack.length - 1) return;
    const target = history.stack[history.index + 1];
    setHistory({ ...history, index: history.index + 1 });
    setPanelParam(serializePanel(target), true);
  }, [history, setPanelParam]);

  const startResize = useCallback((event: ReactPointerEvent) => {
    event.preventDefault();
    const onMove = (move: PointerEvent) => setWidth(clampWidth(window.innerWidth - move.clientX));
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }, []);

  return {
    entry,
    width,
    open,
    close,
    back,
    forward,
    canBack: history.index > 0,
    canForward: history.index < history.stack.length - 1,
    startResize,
  };
}

export type SidePanelState = ReturnType<typeof useSidePanel>;
