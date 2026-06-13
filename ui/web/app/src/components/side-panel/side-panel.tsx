import { useEffect, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { Button } from "../ui/button";

type SidePanelProps = {
  title: ReactNode;
  subtitle?: ReactNode;
  width: number;
  canBack: boolean;
  canForward: boolean;
  onBack: () => void;
  onForward: () => void;
  onClose: () => void;
  onResizeStart: (event: ReactPointerEvent) => void;
  children: ReactNode;
};

export function SidePanel({
  title,
  subtitle,
  width,
  canBack,
  canForward,
  onBack,
  onForward,
  onClose,
  onResizeStart,
  children,
}: SidePanelProps) {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return (
    <aside
      role="complementary"
      aria-label="Detail panel"
      className="fixed inset-y-0 right-0 z-40 flex max-w-full flex-col border-l border-border bg-surface shadow-elev-raised"
      style={{ width }}
    >
      <header className="flex items-center gap-1 border-b border-border-soft px-4 py-3">
        <Button variant="ghost" size="icon" aria-label="Back" disabled={!canBack} onClick={onBack}>
          <ChevronLeft size={15} />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Forward"
          disabled={!canForward}
          onClick={onForward}
        >
          <ChevronRight size={15} />
        </Button>
        <div className="min-w-0 flex-1 pl-1">
          <p className="truncate font-display text-sm font-semibold leading-tight">{title}</p>
          {subtitle && (
            <p className="truncate font-mono text-xs tracking-[0.02em] text-muted">{subtitle}</p>
          )}
        </div>
        <Button variant="ghost" size="icon" aria-label="Close panel" onClick={onClose}>
          <X size={15} />
        </Button>
      </header>
      <div className="flex-1 overflow-y-auto px-4 py-4">{children}</div>
      <div
        role="presentation"
        onPointerDown={onResizeStart}
        className="absolute inset-y-0 left-0 w-1 cursor-col-resize hover:bg-accent/30"
      />
    </aside>
  );
}
