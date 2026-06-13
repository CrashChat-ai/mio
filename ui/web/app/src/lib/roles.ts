import type { Operator } from "./api/types";

export type Role = Operator["role"];

export function roleAllows(actual: Role, required: Role): boolean {
  return roleRank(actual) >= roleRank(required);
}

function roleRank(role: Role): number {
  if (role === "credential-admin") return 3;
  if (role === "operator") return 2;
  return 1;
}
