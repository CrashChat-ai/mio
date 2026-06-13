import { Skeleton } from "../ui/skeleton";
import { TableCell, TableRow } from "../ui/table";

export function DataTableSkeleton({ columns, rows = 6 }: { columns: number; rows?: number }) {
  return (
    <>
      {Array.from({ length: rows }, (_, rowIndex) => (
        <TableRow key={rowIndex} className="hover:bg-transparent">
          {Array.from({ length: columns }, (_, cellIndex) => (
            <TableCell key={cellIndex}>
              <Skeleton className="h-4 w-full max-w-[120px]" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  );
}
