import { queryOptions } from "@tanstack/react-query";
import { queries } from "../lib/query-keys";

export const channelTypesListQuery = queryOptions({
  ...queries.channelTypes.list,
  select: (data) => data.channelTypes,
});
