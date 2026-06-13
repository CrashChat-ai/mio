import type { ChannelType } from "./api/types";

export function formatTime(value: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

export function formatDateTime(value: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

export function flagText(channel: ChannelType): string {
  const flags = [
    channel.supportsThreads ? "threads" : "",
    channel.supportsEdit ? "edit" : "",
    channel.supportsDelete ? "delete" : "",
  ].filter(Boolean);
  return flags.length > 0 ? flags.join(", ") : "read-only";
}
