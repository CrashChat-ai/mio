import type { ReactNode } from "react";
import type { Table } from "@tanstack/react-table";
import { Columns3 } from "lucide-react";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { Input } from "../ui/input";

type DataTableToolbarProps<TData> = {
  table: Table<TData>;
  searchColumn?: string;
  searchPlaceholder?: string;
  actions?: ReactNode;
  bulkActions?: (rows: TData[]) => ReactNode;
};

export function DataTableToolbar<TData>({
  table,
  searchColumn,
  searchPlaceholder = "Filter",
  actions,
  bulkActions,
}: DataTableToolbarProps<TData>) {
  const column = searchColumn ? table.getColumn(searchColumn) : undefined;
  const selectedRows = table.getSelectedRowModel().rows.map((row) => row.original);
  const hideableColumns = table.getAllColumns().filter((col) => col.getCanHide());

  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-border-soft px-5 py-3">
      {column && (
        <Input
          type="search"
          value={(column.getFilterValue() as string | undefined) ?? ""}
          onChange={(event) => column.setFilterValue(event.target.value || undefined)}
          placeholder={searchPlaceholder}
          aria-label={searchPlaceholder}
          className="w-56 max-sm:w-full"
        />
      )}
      {bulkActions && selectedRows.length > 0 && (
        <div className="flex items-center gap-3">
          <span className="font-mono text-xs text-fg-2">{selectedRows.length} selected</span>
          {bulkActions(selectedRows)}
        </div>
      )}
      <div className="ml-auto flex items-center gap-3">
        {actions}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="Toggle columns">
              <Columns3 size={15} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Columns</DropdownMenuLabel>
            {hideableColumns.map((col) => (
              <DropdownMenuCheckboxItem
                key={col.id}
                checked={col.getIsVisible()}
                onCheckedChange={(checked) => col.toggleVisibility(checked === true)}
              >
                {typeof col.columnDef.header === "string" ? col.columnDef.header : col.id}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
