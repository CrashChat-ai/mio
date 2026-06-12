import type { ColumnDef } from "@tanstack/react-table";
import type { Account } from "../../../lib/api/types";
import { Badge } from "../../ui/badge";
import { StatusBadge, statusVariant } from "../../status-badge";

export function accountStatus(account: Account): string {
  return account.disabledAt ? "disabled" : "active";
}

export const accountColumns: ColumnDef<Account>[] = [
  {
    accessorKey: "displayName",
    header: "Name",
    filterFn: (row, _columnId, filterValue) => {
      const query = String(filterValue).toLowerCase();
      return (
        row.original.displayName.toLowerCase().includes(query) ||
        row.original.externalId.toLowerCase().includes(query)
      );
    },
    cell: ({ row }) => (
      <span className="font-medium text-fg">
        {row.original.displayName || row.original.externalId || row.original.id}
      </span>
    ),
  },
  {
    accessorKey: "channelType",
    header: "Channel",
    cell: ({ row }) => <Badge>{row.original.channelType}</Badge>,
  },
  {
    accessorKey: "externalId",
    header: "External ID",
    meta: { cellClassName: "font-mono text-xs tracking-[0.02em] text-fg-2" },
  },
  {
    accessorKey: "rateLimitPerSecond",
    header: "Rate",
    cell: ({ row }) =>
      `${row.original.rateLimitPerSecond || 0}/s ${row.original.rateLimitScope || ""}`.trim(),
    meta: {
      headerClassName: "text-right",
      cellClassName: "text-right font-mono text-xs text-fg-2",
    },
  },
  {
    id: "status",
    accessorFn: (account) => accountStatus(account),
    header: "Status",
    cell: ({ row }) => {
      const status = accountStatus(row.original);
      return <StatusBadge variant={statusVariant(status)}>{status}</StatusBadge>;
    },
  },
];
