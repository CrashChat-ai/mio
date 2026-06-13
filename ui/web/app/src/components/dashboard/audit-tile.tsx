import { Link } from "@tanstack/react-router";
import { Card } from "../ui/card";

export function AuditTile() {
  return (
    <Card aria-label="Audit log" className="flex flex-1 flex-col gap-2 px-5 py-5">
      <div>
        <p className="eyebrow">audit</p>
        <h2 className="font-display text-base font-semibold leading-tight">Audit log</h2>
      </div>
      <div className="grid-texture mt-1 grid flex-1 place-content-center justify-items-center gap-1 rounded-md border border-dashed border-border px-4 py-6 text-center">
        <strong className="text-sm font-medium text-fg-2">No audit events yet</strong>
        <span className="text-xs text-muted">
          Operator actions on tenants, accounts, and credentials will appear here.
        </span>
        <Link to="/audit" className="mt-1 font-mono text-xs text-accent hover:text-accent-hover">
          Open audit →
        </Link>
      </div>
    </Card>
  );
}
