import { useCallback, useRef, useState } from "react";

export function useCopy(revertMs = 1500) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const copy = useCallback(
    async (value: string) => {
      try {
        await navigator.clipboard.writeText(value);
        setCopied(true);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => setCopied(false), revertMs);
      } catch {
        setCopied(false);
      }
    },
    [revertMs],
  );

  return { copied, copy };
}
