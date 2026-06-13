import type { ColumnDef } from "@tanstack/react-table";

export function selectionColumn<TData>(): ColumnDef<TData> {
  return {
    id: "select",
    enableSorting: false,
    enableHiding: false,
    header: ({ table }) => (
      <input
        type="checkbox"
        aria-label="Select all rows"
        className="size-3.5 cursor-pointer accent-accent"
        checked={table.getIsAllPageRowsSelected()}
        onChange={(event) => table.toggleAllPageRowsSelected(event.target.checked)}
      />
    ),
    cell: ({ row }) => (
      <input
        type="checkbox"
        aria-label="Select row"
        className="size-3.5 cursor-pointer accent-accent"
        checked={row.getIsSelected()}
        disabled={!row.getCanSelect()}
        onChange={(event) => row.toggleSelected(event.target.checked)}
        onClick={(event) => event.stopPropagation()}
      />
    ),
    meta: { headerClassName: "w-10", cellClassName: "w-10" },
  };
}
