import { createRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { authedRoute } from "./__root";
import { channelTypesListQuery } from "../queries/channel-types";
import { flagText } from "../lib/format";
import { DataTableSkeleton } from "../components/data-table/data-table-skeleton";
import { EmptyState } from "../components/empty-state";
import { StatusBadge, statusVariant } from "../components/status-badge";
import { PageHead } from "../components/shell/page-head";
import { Badge } from "../components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";

export const channelTypesRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/channel-types",
  staticData: { title: "Channel types" },
  loader: ({ context }) => void context.queryClient.prefetchQuery(channelTypesListQuery),
  component: ChannelTypesPage,
});

function ChannelTypesPage() {
  const { data: channelTypes = [], isLoading, error } = useQuery(channelTypesListQuery);

  return (
    <div className="grid gap-5">
      <PageHead title="Channel types">
        <Badge>{channelTypes.length}</Badge>
      </PageHead>
      <div className="overflow-hidden rounded-lg border border-border bg-surface shadow-elev-raised">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>Slug</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Auth</TableHead>
              <TableHead className="text-right">Rate</TableHead>
              <TableHead>Capabilities</TableHead>
              <TableHead>Attachments</TableHead>
              <TableHead className="text-right">Max text</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <DataTableSkeleton columns={7} />
            ) : channelTypes.length === 0 ? (
              <TableRow className="hover:bg-transparent">
                <TableCell colSpan={7} className="whitespace-normal p-3">
                  {error ? (
                    <EmptyState title="Failed to load channel types" description={error.message} />
                  ) : (
                    <EmptyState
                      title="No channel types registered"
                      description="Registered channel adapters and their capabilities appear here."
                    />
                  )}
                </TableCell>
              </TableRow>
            ) : (
              channelTypes.map((channel) => (
                <TableRow key={channel.slug}>
                  <TableCell className="font-mono text-xs tracking-[0.02em] text-fg">
                    {channel.slug}
                  </TableCell>
                  <TableCell>
                    <StatusBadge variant={statusVariant(channel.status)}>
                      {channel.status}
                    </StatusBadge>
                  </TableCell>
                  <TableCell className="text-fg-2">{channel.authKind || "none"}</TableCell>
                  <TableCell className="text-right font-mono text-xs text-fg-2">
                    {channel.rateLimitPerSecond || 0}/s {channel.rateLimitScope}
                  </TableCell>
                  <TableCell className="text-fg-2">{flagText(channel)}</TableCell>
                  <TableCell className="text-fg-2">
                    {(channel.allowedAttachmentKinds ?? []).join(", ") || "none"}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs text-fg-2">
                    {channel.maxTextBytes}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

export const channelTypesTree = channelTypesRoute;
