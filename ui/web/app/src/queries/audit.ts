import { queryOptions } from "@tanstack/react-query";
import { queries } from "../lib/query-keys";

export const auditListQuery = queryOptions({
  ...queries.audit.list,
  select: (data) => data.events,
});
