import { createContext, useContext, type ReactNode } from "react";
import type { Operator, SessionResponse } from "../lib/api/types";
import type { Role } from "../lib/roles";

type SessionContextValue = {
  session: SessionResponse;
  operator?: Operator;
  role: Role;
};

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({
  session,
  children,
}: {
  session: SessionResponse;
  children: ReactNode;
}) {
  const value: SessionContextValue = {
    session,
    operator: session.operator,
    role: session.operator?.role ?? "viewer",
  };
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
  const value = useContext(SessionContext);
  if (!value) {
    throw new Error("useSession must be used within SessionProvider");
  }
  return value;
}
