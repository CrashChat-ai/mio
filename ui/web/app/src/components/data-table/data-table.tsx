import { useMemo, useState, type ReactNode } from "react";
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnFiltersState,
  type OnChangeFn,
  type RowData,
  type SortingState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react";
import { cn } from "../../lib/cn";
import { Button } from "../ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { DataTableSkeleton } from "./data-table-skeleton";
import { DataTableToolbar } from "./data-table-toolbar";
import { selectionColumn } from "./columns/selection-column";

declare module "@tanstack/react-table" {
  interface ColumnMeta<TData extends RowData, TValue> {
    headerClassName?: string;
    cellClassName?: string;
  }
}

const PAGE_SIZE = 25;

type DataTableProps<TData> = {
  columns: ColumnDef<TData>[];
  data: TData[];
  isLoading?: boolean;
  emptyState?: ReactNode;
  searchColumn?: string;
  searchPlaceholder?: string;
  toolbarActions?: ReactNode;
  bulkActions?: (rows: TData[]) => ReactNode;
  enableSelection?: boolean;
  onRowClick?: (row: TData) => void;
  getRowId?: (row: TData) => string;
  activeRowId?: string;
  columnFilters?: ColumnFiltersState;
  onColumnFiltersChange?: OnChangeFn<ColumnFiltersState>;
};

export function DataTable<TData>({
  columns,
  data,
  isLoading = false,
  emptyState,
  searchColumn,
  searchPlaceholder,
  toolbarActions,
  bulkActions,
  enableSelection = false,
  onRowClick,
  getRowId,
  activeRowId,
  columnFilters,
  onColumnFiltersChange,
}: DataTableProps<TData>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [internalFilters, setInternalFilters] = useState<ColumnFiltersState>([]);
  const [columnVisibility, setColumnVisibility] = useState({});
  const [rowSelection, setRowSelection] = useState({});

  const allColumns = useMemo(
    () => (enableSelection ? [selectionColumn<TData>(), ...columns] : columns),
    [columns, enableSelection],
  );

  const table = useReactTable({
    data,
    columns: allColumns,
    state: {
      sorting,
      columnFilters: columnFilters ?? internalFilters,
      columnVisibility,
      rowSelection,
    },
    onSortingChange: setSorting,
    onColumnFiltersChange: onColumnFiltersChange ?? setInternalFilters,
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange: setRowSelection,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: false,
    initialState: { pagination: { pageSize: PAGE_SIZE } },
    enableRowSelection: enableSelection,
    getRowId,
  });

  const rows = table.getRowModel().rows;
  const visibleColumnCount = table.getVisibleLeafColumns().length;
  const filteredCount = table.getFilteredRowModel().rows.length;
  const { pageIndex, pageSize } = table.getState().pagination;

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface shadow-elev-raised">
      <DataTableToolbar
        table={table}
        searchColumn={searchColumn}
        searchPlaceholder={searchPlaceholder}
        actions={toolbarActions}
        bulkActions={bulkActions}
      />
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className="hover:bg-transparent">
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id} className={header.column.columnDef.meta?.headerClassName}>
                  {header.isPlaceholder ? null : header.column.getCanSort() ? (
                    <button
                      type="button"
                      onClick={header.column.getToggleSortingHandler()}
                      className="inline-flex cursor-pointer items-center gap-1.5 uppercase transition-colors hover:text-fg focus-visible:shadow-focus-ring focus-visible:outline-none"
                    >
                      {flexRender(header.column.columnDef.header, header.getContext())}
                      <SortIcon direction={header.column.getIsSorted()} />
                    </button>
                  ) : (
                    flexRender(header.column.columnDef.header, header.getContext())
                  )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <DataTableSkeleton columns={visibleColumnCount} />
          ) : rows.length === 0 ? (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={visibleColumnCount} className="whitespace-normal p-3">
                {emptyState ?? <p className="py-6 text-center text-sm text-muted">No results</p>}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow
                key={row.id}
                data-state={
                  row.getIsSelected() || (activeRowId !== undefined && row.id === activeRowId)
                    ? "selected"
                    : undefined
                }
                onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                className={cn(onRowClick && "cursor-pointer")}
              >
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id} className={cell.column.columnDef.meta?.cellClassName}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
      {!isLoading && filteredCount > pageSize && (
        <div className="flex items-center justify-between border-t border-border-soft px-5 py-3">
          <span className="font-mono text-xs text-muted">
            {pageIndex * pageSize + 1}–{Math.min((pageIndex + 1) * pageSize, filteredCount)} of{" "}
            {filteredCount}
          </span>
          <div className="flex items-center gap-2">
            <Button size="xs" disabled={!table.getCanPreviousPage()} onClick={() => table.previousPage()}>
              Previous
            </Button>
            <Button size="xs" disabled={!table.getCanNextPage()} onClick={() => table.nextPage()}>
              Next
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function SortIcon({ direction }: { direction: false | "asc" | "desc" }) {
  if (direction === "asc") return <ArrowUp size={12} />;
  if (direction === "desc") return <ArrowDown size={12} />;
  return <ChevronsUpDown size={12} className="text-fg-faint" />;
}
