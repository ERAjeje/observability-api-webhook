import { memo, lazy, Suspense, useState } from "react";
import type { PublicEndpoint } from "../lib/types";
import { useSummary } from "../hooks/queries";
import { fmtLatency, fmtUptime } from "../lib/api";
import { StatusBadge } from "./StatusBadge";
import { Spinner } from "./Spinner";

// ChartPanel (Recharts) é carregado sob demanda — a status page não baixa o
// bundle de charts no boot (Lighthouse ≥ 90 — RNF-015).
const ChartPanel = lazy(() => import("./ChartPanel").then((m) => ({ default: m.ChartPanel })));

export const EndpointCard = memo(function EndpointCard({ ep }: { ep: PublicEndpoint }) {
  const [open, setOpen] = useState(false);
  const summary = useSummary(ep.id);

  return (
    <div className="rounded-xl border border-white/10 bg-slate-900/70 p-4 transition-shadow hover:border-white/20">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-slate-100">{ep.name}</p>
          <p className="mt-0.5 text-xs text-slate-400">
            {summary.data ? (
              <>
                média <span className="text-slate-200">{fmtLatency(summary.data.latency_ms.avg)}</span>
                {" · "}P95{" "}
                <span className="text-slate-200">{fmtLatency(summary.data.latency_ms.p95)}</span>
                {" · "}uptime 24h{" "}
                <span className="text-slate-200">{fmtUptime(summary.data.uptime.day)}</span>
              </>
            ) : (
              "aguardando dados…"
            )}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <StatusBadge status={ep.status} />
          <button
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            className="rounded-lg border border-white/10 px-2 py-1 text-xs text-slate-300 hover:bg-white/5"
          >
            {open ? "ocultar" : "gráfico"}
          </button>
        </div>
      </div>

      {open && (
        <div className="mt-4 border-t border-white/10 pt-4">
          <Suspense fallback={<Spinner label="Carregando gráficos…" />}>
            <ChartPanel ep={ep} />
          </Suspense>
        </div>
      )}
    </div>
  );
});