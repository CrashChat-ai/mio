import { Button } from "../ui/button";
import { CodeChip } from "./code-chip";

const GATEWAY_WEBHOOK_PORT = "18080";

function gatewayWebhookUrl(channelType: string): string {
  const { protocol, hostname } = window.location;
  return `${protocol}//${hostname}:${GATEWAY_WEBHOOK_PORT}/webhooks/${channelType}`;
}

type WebhookSetupStepProps = {
  channelType: string;
  onNext: () => void;
};

export function WebhookSetupStep({ channelType, onNext }: WebhookSetupStepProps) {
  const url = gatewayWebhookUrl(channelType);
  return (
    <div className="grid gap-4">
      <div className="grid gap-1.5">
        <span className="eyebrow">Gateway webhook URL</span>
        <CodeChip value={url} label="Webhook URL" />
      </div>
      <p className="text-sm text-muted">
        Paste this URL into your {channelType} extension. The live per-account URL appears in the
        account&rsquo;s Webhook tab once the install completes.
      </p>
      <div>
        <Button type="button" variant="primary" onClick={onNext}>
          Continue
        </Button>
      </div>
    </div>
  );
}
