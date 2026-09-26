import { lazy, Suspense } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "./lib/query";
import { ProtectedRoute } from "./components/ProtectedRoute";
import PublicStatus from "./pages/PublicStatus";
import Login from "./pages/Login";

// Code-splitting: as páginas admin (com Recharts) só carregam quando acessadas;
// a status page pública mantém o bundle enxuto (Lighthouse ≥ 90 — RNF-015).
const AdminShell = lazy(() => import("./pages/admin/AdminShell"));
const EndpointsPage = lazy(() => import("./pages/admin/Endpoints"));
const GroupsPage = lazy(() => import("./pages/admin/Groups"));
const LogsPage = lazy(() => import("./pages/admin/Logs"));
const StatsPage = lazy(() => import("./pages/admin/Stats"));
const AlertsPage = lazy(() => import("./pages/admin/Alerts"));
const NotificationsPage = lazy(() => import("./pages/admin/Notifications"));

function AdminRoutes() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-screen items-center justify-center bg-slate-950 text-sm text-slate-400">
          Carregando painel…
        </div>
      }
    >
      <ProtectedRoute>
        <AdminShell />
      </ProtectedRoute>
    </Suspense>
  );
}

function ChildRoutes() {
  return (
    <Suspense fallback={<div className="p-6 text-sm text-slate-400">Carregando…</div>}>
      <Routes>
        <Route index element={<EndpointsPage />} />
        <Route path="endpoints" element={<EndpointsPage />} />
        <Route path="groups" element={<GroupsPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="stats" element={<StatsPage />} />
        <Route path="alerts" element={<AlertsPage />} />
        <Route path="notifications" element={<NotificationsPage />} />
      </Routes>
    </Suspense>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<PublicStatus />} />
          <Route path="/admin/login" element={<Login />} />
          <Route path="/admin" element={<AdminRoutes />}>
            <Route path="*" element={<ChildRoutes />} />
          </Route>
          <Route path="*" element={<PublicStatus />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}