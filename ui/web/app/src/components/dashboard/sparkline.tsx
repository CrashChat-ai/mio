import { cn } from "../../lib/cn";

const W = 84;
const H = 24;

function points(series: number[]): string {
  if (series.length === 0) return "";
  if (series.length === 1) return `0,${H / 2} ${W},${H / 2}`;
  const min = Math.min(...series);
  const max = Math.max(...series);
  const span = max - min || 1;
  return series
    .map((value, index) => {
      const x = (index / (series.length - 1)) * W;
      const y = H - 2 - ((value - min) / span) * (H - 4);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

export function Sparkline({
  series,
  tone = "muted",
}: {
  series: number[];
  tone?: "muted" | "alert" | "flat";
}) {
  const flat = tone === "flat" || series.length < 2;
  return (
    <svg
      width={W}
      height={H}
      viewBox={`0 0 ${W} ${H}`}
      aria-hidden="true"
      className={cn(
        tone === "alert" ? "text-warn" : "text-muted",
        flat && "text-muted/65",
      )}
    >
      <polyline
        points={flat ? `0,${H / 2} ${W},${H / 2}` : points(series)}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
      />
    </svg>
  );
}
