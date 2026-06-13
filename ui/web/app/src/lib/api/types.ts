import type { components } from "./generated/schema";

type Schemas = components["schemas"];

export type Operator = Schemas["Operator"];
export type SessionResponse = Schemas["SessionResponse"];
export type Tenant = Schemas["Tenant"];
export type Account = Schemas["Account"];
export type ChannelType = Schemas["ChannelType"];
export type TailMessage = Schemas["TailMessage"];
export type CredentialMetadata = Schemas["CredentialMetadata"];
export type WebhookInfo = Schemas["WebhookInfo"];
export type ConsumerHealth = Schemas["ConsumerHealth"];
export type AuditEvent = Schemas["AuditEvent"];

export type LoadState = "idle" | "loading" | "ready" | "error";
