import { Button } from "../ui/button";
import { useCopy } from "./use-copy";

type CodeChipProps = {
  value: string;
  label: string;
};

export function CodeChip({ value, label }: CodeChipProps) {
  const { copied, copy } = useCopy();
  return (
    <div className="flex items-center gap-2 rounded-md border border-border bg-bg py-2 pl-3 pr-2">
      <code className="min-w-0 flex-1 truncate font-mono text-xs text-fg-2">{value}</code>
      <Button
        type="button"
        size="xs"
        className="shrink-0"
        onClick={() => void copy(value)}
      >
        {copied ? "Copied" : "Copy"}
      </Button>
      <span role="status" aria-live="polite" className="sr-only">
        {copied ? `${label} copied` : ""}
      </span>
    </div>
  );
}
