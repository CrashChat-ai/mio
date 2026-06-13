import { Pause, Play } from "lucide-react";
import { Button } from "../ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../ui/select";
import { REFETCH_OPTIONS, useRefetchInterval } from "./refetch-interval";

function label(ms: number): string {
  return ms >= 60000 ? `${ms / 60000}m` : `${ms / 1000}s`;
}

export function RefetchControls() {
  const { interval, setInterval, frozen, setFrozen, suspended } = useRefetchInterval();

  return (
    <div className="flex items-center gap-2">
      <Select value={String(interval)} onValueChange={(v) => setInterval(Number(v))}>
        <SelectTrigger aria-label="Refresh interval" className="w-28">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {REFETCH_OPTIONS.map((ms) => (
            <SelectItem key={ms} value={String(ms)}>
              Every {label(ms)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        variant="secondary"
        size="default"
        aria-pressed={frozen}
        onClick={() => setFrozen(!frozen)}
      >
        {frozen ? <Play size={14} /> : <Pause size={14} />}
        {frozen ? "Resume" : "Freeze"}
      </Button>
      {suspended && !frozen && (
        <span className="font-mono text-xs text-muted">paused</span>
      )}
    </div>
  );
}
