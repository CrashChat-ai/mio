import type { TailMessage } from "../../lib/api/types";
import { formatTime } from "../../lib/format";

export function TailRow({
  message,
  onClick,
}: {
  message: TailMessage;
  onClick?: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onClick}
        className="grid w-full grid-cols-[auto_auto_auto_minmax(0,1fr)_auto] items-center gap-3 border-b border-border-soft px-5 py-3 text-left text-sm transition-colors last:border-0 hover:bg-surface-2 max-[560px]:grid-cols-[auto_minmax(0,1fr)]"
      >
        <span className="inline-flex shrink-0 items-center rounded-sm bg-surface-2 px-2 py-1 font-mono text-xs text-fg-2">
          {message.channelType}
        </span>
        <span className="max-w-[16ch] truncate font-mono text-xs text-muted">
          {message.conversationId}
        </span>
        <span className="truncate font-medium text-fg max-[560px]:hidden">
          {message.senderDisplay}
        </span>
        <span className="truncate text-fg-2 max-[560px]:col-span-2">
          <span className="mr-2 font-medium text-fg min-[561px]:hidden">
            {message.senderDisplay}
          </span>
          {message.text}
        </span>
        <span className="font-mono text-xs text-muted max-[560px]:col-span-2 max-[560px]:text-left">
          {formatTime(message.receivedAt)}
        </span>
      </button>
    </li>
  );
}
