import { useQuery } from "@tanstack/react-query";
import { channelTypesListQuery } from "../../queries/channel-types";
import { tenantsListQuery } from "../../queries/tenants";
import { Button } from "../ui/button";
import { Label } from "../ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../ui/select";

type ChannelTypeStepProps = {
  tenantId: string;
  channelType: string;
  onTenantChange: (value: string) => void;
  onChannelTypeChange: (value: string) => void;
  onNext: () => void;
};

export function ChannelTypeStep({
  tenantId,
  channelType,
  onTenantChange,
  onChannelTypeChange,
  onNext,
}: ChannelTypeStepProps) {
  const { data: tenants = [] } = useQuery(tenantsListQuery);
  const { data: channelTypes = [] } = useQuery(channelTypesListQuery);

  return (
    <div className="grid gap-4">
      <div className="grid gap-1.5">
        <Label htmlFor="onboarding-tenant">Tenant</Label>
        <Select value={tenantId} onValueChange={onTenantChange}>
          <SelectTrigger id="onboarding-tenant" className="w-full">
            <SelectValue placeholder="Select a tenant" />
          </SelectTrigger>
          <SelectContent>
            {tenants.map((tenant) => (
              <SelectItem key={tenant.id} value={tenant.id}>
                {tenant.displayName || tenant.slug}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="onboarding-channel">Channel type</Label>
        <Select value={channelType} onValueChange={onChannelTypeChange}>
          <SelectTrigger id="onboarding-channel" className="w-full">
            <SelectValue placeholder="Select a channel type" />
          </SelectTrigger>
          <SelectContent>
            {channelTypes.map((channel) => (
              <SelectItem key={channel.slug} value={channel.slug}>
                {channel.slug}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div>
        <Button
          type="button"
          variant="primary"
          disabled={tenantId === "" || channelType === ""}
          onClick={onNext}
        >
          Continue
        </Button>
      </div>
    </div>
  );
}
