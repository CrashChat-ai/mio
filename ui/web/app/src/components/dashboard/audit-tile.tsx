import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { auditListQuery } from "../../queries/audit";
import { formatDateTime } from "../../lib/format";
import { Card } from "../ui/card";

export function AuditTile() {
  const { data: events = [] } = useQuery(auditListQuery);
  const recent = events.slice(0, 4);

  return (
    <Card aria-label="Audit log" className="flex flex-1 flex-col gap-2 px-5 py-5">
      <div className="flex items-baseline justify-between">
        <div>
          <p className="eyebrow">audit</p>
          <h2 className="font-display text-base font-semibold leading-tight">Audit log</h2>
        </div>
        <Link to="/audit" className="font-mono text-xs text-accent hover:text-accent-hover">
          Open audit →
        </Link>
      </div>
      {recent.length === 0 ? (
        <div className="grid-texture mt-1 grid flex-1 place-content-center justify-items-center gap-1 rounded-md border border-dashed border-border px-4 py-6 text-center">
          <strong className="text-sm font-medium text-fg-2">No audit events yet</strong>
          <span className="text-xs text-muted">
            Operator actions on tenants, accounts, and credentials will appear here.
          </span>
        </div>
      ) : (
        <ul className="mt-1 grid gap-1.5">
          {recent.map((event) => (
            <li
              key={`${event.createdAt}:${event.action}:${event.targetId}`}
              className="flex items-baseline justify-between gap-3 text-xs"
            >
              <span className="truncate font-mono text-fg-2">{event.action}</span>
              <span className="shrink-0 font-mono text-muted">
                {formatDateTime(event.createdAt)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
