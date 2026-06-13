import { useMemo, useState, type ReactNode } from "react";
import { Pause, Play } from "lucide-react";
import type { TailMessage } from "../../lib/api/types";
import { EmptyState } from "../empty-state";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { useSseTail } from "./use-sse-tail";
import { TailRow } from "./tail-row";
import { TailStatusPill } from "./tail-status-pill";

function matches(message: TailMessage, term: string): boolean {
  const q = term.toLowerCase();
  return (
    message.text.toLowerCase().includes(q) ||
    message.senderDisplay.toLowerCase().includes(q) ||
    message.conversationId.toLowerCase().includes(q)
  );
}

export function LiveTailView({
  accountId,
  conversationId,
  eyebrow = "messages_inbound",
  empty,
  onRowClick,
}: {
  accountId?: string;
  conversationId?: string;
  eyebrow?: string;
  empty?: ReactNode;
  onRowClick?: (message: TailMessage) => void;
}) {
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  const [announce, setAnnounce] = useState(false);
  const { messages, status } = useSseTail({ accountId, conversationId, paused });

  const visible = useMemo(
    () => (filter.trim() === "" ? messages : messages.filter((m) => matches(m, filter.trim()))),
    [messages, filter],
  );

  return (
    <section
      aria-label="Live tail"
      className="flex flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-elev-raised"
    >
      <header className="flex items-center justify-between gap-4 border-b border-border-soft px-5 py-4">
        <div>
          <p className="eyebrow">{eyebrow}</p>
          <h2 className="font-display text-base font-semibold leading-tight">Live tail</h2>
        </div>
        <TailStatusPill status={status} />
      </header>
      <div className="flex flex-wrap items-center gap-2 border-b border-border-soft px-5 py-3">
        <Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter stream"
          aria-label="Filter live tail"
          className="max-w-56"
        />
        <Button
          variant="secondary"
          aria-pressed={paused}
          disabled={!accountId}
          onClick={() => setPaused((p) => !p)}
        >
          {paused ? <Play size={14} /> : <Pause size={14} />}
          {paused ? "Resume" : "Pause"}
        </Button>
        <Button
          variant="ghost"
          size="xs"
          aria-pressed={announce}
          className="ml-auto"
          onClick={() => setAnnounce((a) => !a)}
        >
          Announce {announce ? "on" : "off"}
        </Button>
      </div>
      <div
        role="log"
        aria-live={announce ? "polite" : "off"}
        aria-label="Live tail messages"
        className="min-h-0 flex-1 overflow-y-auto"
      >
        {visible.length === 0 ? (
          <div className="p-3">
            {empty ?? (
              <EmptyState
                title={accountId ? "Waiting for messages" : "Select an account"}
                description={
                  accountId
                    ? "New messages on this account stream in here."
                    : "Pick an account to start tailing its messages."
                }
              />
            )}
          </div>
        ) : (
          <ol className="m-0 list-none p-0">
            {visible.map((message) => (
              <TailRow
                key={message.id}
                message={message}
                onClick={onRowClick ? () => onRowClick(message) : undefined}
              />
            ))}
          </ol>
        )}
      </div>
    </section>
  );
}
