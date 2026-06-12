import type { ColumnDef } from "@tanstack/react-table";
import type { Tenant } from "../../../lib/api/types";
import { formatDateTime } from "../../../lib/format";
import { StatusBadge, statusVariant } from "../../status-badge";

export const tenantColumns: ColumnDef<Tenant>[] = [
  {
    accessorKey: "displayName",
    header: "Name",
    filterFn: (row, _columnId, filterValue) => {
      const query = String(filterValue).toLowerCase();
      return (
        row.original.displayName.toLowerCase().includes(query) ||
        row.original.slug.toLowerCase().includes(query)
      );
    },
    cell: ({ row }) => (
      <span className="font-medium text-fg">{row.original.displayName || row.original.slug}</span>
    ),
  },
  {
    accessorKey: "slug",
    header: "Slug",
    meta: { cellClassName: "font-mono text-xs tracking-[0.02em] text-fg-2" },
  },
  {
    accessorKey: "status",
    header: "Status",
    cell: ({ row }) => (
      <StatusBadge variant={statusVariant(row.original.status)}>{row.original.status}</StatusBadge>
    ),
  },
  {
    accessorKey: "createdAt",
    header: "Created",
    cell: ({ row }) => formatDateTime(row.original.createdAt),
    meta: { cellClassName: "font-mono text-xs text-fg-2" },
  },
];
