// Shell do dashboard admin: header + navegação + logout (T4.3).
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import clsx from "clsx";
import { setToken } from "../../lib/api";

const NAV = [
  { to: "/admin/endpoints", label: "Endpoints" },
  { to: "/admin/groups", label: "Grupos" },
  { to: "/admin/logs", label: "Logs" },
  { to: "/admin/stats", label: "Estatísticas" },
  { to: "/admin/alerts", label: "Alertas" },
  { to: "/admin/notifications", label: "Notificações" },
];

export default function AdminShell() {
  const nav = useNavigate();
  return (
    <div className="min-h-screen bg-slate-950 text-slate-200">
      <header className="sticky top-0 z-10 border-b border-white/10 bg-slate-950/90 backdrop-blur">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-3">
          <div className="flex items-baseline gap-3">
            <span className="text-sm font-bold text-slate-100">Monitor</span>
            <nav className="flex gap-1" aria-label="Admin">
              {NAV.map((n) => (
                <NavLink
                  key={n.to}
                  to={n.to}
                  className={({ isActive }) =>
                    clsx(
                      "rounded-lg px-2.5 py-1 text-sm",
                      isActive ? "bg-sky-500/15 text-sky-300" : "text-slate-400 hover:text-slate-200",
                    )
                  }
                >
                  {n.label}
                </NavLink>
              ))}
            </nav>
          </div>
          <div className="flex items-center gap-3">
            <NavLink to="/" className="text-xs text-slate-500 hover:text-slate-300">
              Status page
            </NavLink>
            <button
              onClick={() => {
                setToken(null);
                nav("/admin/login", { replace: true });
              }}
              className="rounded-lg border border-white/10 px-2.5 py-1 text-xs text-slate-400 hover:bg-white/5 hover:text-slate-200"
            >
              Sair
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  );
}