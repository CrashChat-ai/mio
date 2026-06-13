import { useId, useState } from "react";
import { Eye, EyeOff } from "lucide-react";
import { Button } from "../ui/button";
import { useCopy } from "./use-copy";

type SecretCopierProps = {
  value: string;
  label: string;
};

export function SecretCopier({ value, label }: SecretCopierProps) {
  const [revealed, setRevealed] = useState(false);
  const { copied, copy } = useCopy();
  const id = useId();
  return (
    <div className="grid gap-1.5">
      <span id={id} className="eyebrow">
        {label}
      </span>
      <div className="flex items-center gap-2 rounded-md border border-border bg-bg py-2 pl-3 pr-2">
        <code
          aria-labelledby={id}
          className="min-w-0 flex-1 truncate font-mono text-xs text-fg-2"
        >
          {revealed ? value : "•".repeat(Math.min(value.length, 40))}
        </code>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="shrink-0"
          aria-label={revealed ? "Hide value" : "Reveal value"}
          aria-pressed={revealed}
          onClick={() => setRevealed((on) => !on)}
        >
          {revealed ? <EyeOff size={14} /> : <Eye size={14} />}
        </Button>
        <Button type="button" size="xs" className="shrink-0" onClick={() => void copy(value)}>
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <span role="status" aria-live="polite" className="sr-only">
        {copied ? `${label} copied` : ""}
      </span>
    </div>
  );
}
