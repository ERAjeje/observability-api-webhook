// Proteção das rotas admin (T4.3): sem token → redirect para login.
import { Navigate, useLocation } from "react-router-dom";
import type { ReactNode } from "react";
import { getToken } from "../lib/api";

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const loc = useLocation();
  if (!getToken()) {
    return <Navigate to={`/admin/login?next=${encodeURIComponent(loc.pathname)}`} replace />;
  }
  return <>{children}</>;
}