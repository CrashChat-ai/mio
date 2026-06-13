import type { TailMessage } from "../../lib/api/types";
import { formatDateTime } from "../../lib/format";
import { SidePanel } from "../side-panel/side-panel";

const NOOP = () => {};

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid gap-1 border-b border-border-soft py-3 last:border-0">
      <span className="font-mono text-xs uppercase tracking-[0.08em] text-muted">{label}</span>
      <span className={mono ? "font-mono text-xs text-fg-2" : "text-sm text-fg"}>{value || "—"}</span>
    </div>
  );
}

export function TailMessagePanel({
  message,
  width,
  onClose,
  onResizeStart,
}: {
  message: TailMessage;
  width: number;
  onClose: () => void;
  onResizeStart: React.ComponentProps<typeof SidePanel>["onResizeStart"];
}) {
  return (
    <SidePanel
      title={message.senderDisplay || "Message"}
      subtitle={message.id}
      width={width}
      canBack={false}
      canForward={false}
      onBack={NOOP}
      onForward={NOOP}
      onClose={onClose}
      onResizeStart={onResizeStart}
    >
      <Field label="Channel type" value={message.channelType} mono />
      <Field label="Conversation" value={message.conversationId} mono />
      <Field label="Sender" value={message.senderDisplay} />
      <Field label="Received" value={formatDateTime(message.receivedAt)} mono />
      <div className="grid gap-1 py-3">
        <span className="font-mono text-xs uppercase tracking-[0.08em] text-muted">Text</span>
        <p className="whitespace-pre-wrap break-words text-sm text-fg">{message.text || "—"}</p>
      </div>
    </SidePanel>
  );
}
