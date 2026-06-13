import { useCallback, useEffect, useRef, useState } from "react";
import { apiUrl } from "../../lib/api/config";
import type { TailMessage } from "../../lib/api/types";

const CAP = 80;
const BASE_BACKOFF = 1000;
const MAX_BACKOFF = 30000;

export type TailStatus = "idle" | "streaming" | "paused" | "reconnecting" | "error";

type Args = { accountId?: string; conversationId?: string; paused: boolean };

function backoffDelay(attempt: number): number {
  const capped = Math.min(MAX_BACKOFF, BASE_BACKOFF * 2 ** attempt);
  return capped / 2 + Math.random() * (capped / 2);
}

export function useSseTail({ accountId, conversationId, paused }: Args) {
  const [messages, setMessages] = useState<TailMessage[]>([]);
  const [status, setStatus] = useState<TailStatus>("idle");
  const sourceRef = useRef<EventSource | null>(null);
  const timerRef = useRef<number | null>(null);
  const attemptRef = useRef(0);

  const teardown = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    sourceRef.current?.close();
    sourceRef.current = null;
  }, []);

  const connect = useCallback(() => {
    if (!accountId) return;
    teardown();
    const params = new URLSearchParams({ account_id: accountId });
    if (conversationId && conversationId.trim() !== "") {
      params.set("conversation_id", conversationId.trim());
    }
    const source = new EventSource(apiUrl(`/api/admin/messages/tail?${params.toString()}`));
    sourceRef.current = source;
    setStatus(attemptRef.current === 0 ? "streaming" : "reconnecting");

    source.addEventListener("open", () => {
      attemptRef.current = 0;
      setStatus("streaming");
    });
    source.addEventListener("message", (event) => {
      try {
        const msg = JSON.parse((event as MessageEvent).data) as TailMessage;
        setMessages((prev) => [msg, ...prev].slice(0, CAP));
      } catch {
        /* drop malformed frame */
      }
    });
    const reconnect = () => {
      source.close();
      if (sourceRef.current !== source) return;
      sourceRef.current = null;
      const delay = backoffDelay(attemptRef.current);
      attemptRef.current += 1;
      setStatus("reconnecting");
      timerRef.current = window.setTimeout(connect, delay);
    };
    source.addEventListener("error", reconnect);
  }, [accountId, conversationId, teardown]);

  useEffect(() => {
    if (paused || !accountId) {
      teardown();
      setStatus(paused && accountId ? "paused" : "idle");
      return;
    }
    attemptRef.current = 0;
    connect();
    return teardown;
  }, [paused, accountId, conversationId, connect, teardown]);

  const clear = useCallback(() => setMessages([]), []);

  return { messages, status, clear };
}
