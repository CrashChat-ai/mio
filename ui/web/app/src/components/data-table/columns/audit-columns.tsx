import type { ColumnDef } from "@tanstack/react-table";
import type { AuditEvent } from "../../../lib/api/types";
import { formatDateTime } from "../../../lib/format";
import { StatusBadge, type StatusVariant } from "../../status-badge";

function resultVariant(result: string): StatusVariant {
  if (result === "success") return "ok";
  if (result === "denied") return "warn";
  return "danger";
}

export const auditColumns: ColumnDef<AuditEvent>[] = [
  {
    accessorKey: "createdAt",
    header: "When",
    cell: ({ row }) => formatDateTime(row.original.createdAt),
    meta: { cellClassName: "font-mono text-xs text-fg-2 whitespace-nowrap" },
  },
  {
    accessorKey: "operatorEmail",
    header: "Operator",
    cell: ({ row }) => (
      <div className="grid gap-0.5">
        <span className="text-fg">{row.original.operatorEmail}</span>
        <span className="font-mono text-[0.6875rem] uppercase tracking-[0.06em] text-muted">
          {row.original.operatorRole}
        </span>
      </div>
    ),
  },
  {
    accessorKey: "action",
    header: "Action",
    meta: { cellClassName: "font-mono text-xs text-fg-2" },
  },
  {
    accessorKey: "targetId",
    header: "Target",
    cell: ({ row }) => (
      <span className="font-mono text-xs text-fg-2">
        {row.original.targetType}
        {row.original.targetId ? `:${row.original.targetId}` : ""}
      </span>
    ),
  },
  {
    accessorKey: "result",
    header: "Result",
    cell: ({ row }) => (
      <StatusBadge variant={resultVariant(row.original.result)}>{row.original.result}</StatusBadge>
    ),
  },
];
