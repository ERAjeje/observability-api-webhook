// Estatísticas — séries de latência/uptime por endpoint (RF-020, T4.4).
import { useState } from "react";
import { useEndpoints, useStatsSeries, useStatsSummary } from "../../hooks/queries";
import { LatencyChart, UptimeChart } from "../../components/charts";
import { Spinner } from "../../components/Spinner";
import { fmtLatency, fmtUptime } from "../../lib/api";
import clsx from "clsx";

type Period = "24h" | "7d" | "30d";
const PERIODS: { key: Period; bucket: "minute" | "hour" | "day" }[] = [
  { key: "24h", bucket: "minute" },
  { key: "7d", bucket: "hour" },
  { key: "30d", bucket: "day" },
];

export default function StatsPage() {
  const { data: eps } = useEndpoints();
  const [epId, setEpId] = useState<string>("");
  const [period, setPeriod] = useState<Period>("24h");
  const bucket = PERIODS.find((p) => p.key === period)!.bucket;

  const id = epId ? Number(epId) : null;
  const series = useStatsSeries(id, bucket);
  const summary = useStatsSummary(id);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold text-slate-100">Estatísticas (rollups)</h1>
        <div className="flex items-center gap-2">
          <select
            value={epId}
            onChange={(e) => setEpId(e.target.value)}
            className="rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
          >
            <option value="">Escolha um endpoint…</option>
            {(eps?.items ?? []).map((ep) => (
              <option key={ep.id} value={ep.id}>{ep.name}</option>
            ))}
          </select>
          <div className="flex gap-1 rounded-lg border border-white/10 p-0.5">
            {PERIODS.map(({ key }) => (
              <button
                key={key}
                onClick={() => setPeriod(key)}
                className={clsx(
                  "rounded-md px-2.5 py-1 text-xs font-medium",
                  period === key ? "bg-sky-500/20 text-sky-300" : "text-slate-400 hover:text-slate-200",
                )}
              >
                {key}
              </button>
            ))}
          </div>
        </div>
      </div>

      {!id && <p className="text-sm text-slate-500">Selecione um endpoint para ver latência e uptime no período.</p>}

      {id && summary.data && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {[
            ["Uptime 24h", fmtUptime(summary.data.uptime.day)],
            ["Uptime 7d", fmtUptime(summary.data.uptime.week)],
            ["Uptime 30d", fmtUptime(summary.data.uptime.month)],
            ["P95 (24h)", fmtLatency(summary.data.latency_ms.p95)],
          ].map(([label, value]) => (
            <div key={label} className="rounded-xl border border-white/10 bg-slate-900/60 p-3">
              <p className="text-xs text-slate-500">{label}</p>
              <p className="mt-1 text-lg font-semibold text-slate-100">{value}</p>
            </div>
          ))}
        </div>
      )}

      {id && (
        <div className="space-y-4 rounded-xl border border-white/10 bg-slate-900/40 p-4">
          {series.isLoading ? (
            <Spinner />
          ) : (
            <>
              {(series.data?.items ?? []).length === 0 ? (
                <p className="text-sm text-slate-500">Sem rollups no período.</p>
              ) : (
                <>
                  <div>
                    <p className="mb-1 text-xs uppercase tracking-wide text-slate-500">Latência</p>
                    <LatencyChart data={series.data?.items ?? []} height={220} />
                  </div>
                  <div>
                    <p className="mb-1 text-xs uppercase tracking-wide text-slate-500">Uptime</p>
                    <UptimeChart data={series.data?.items ?? []} height={160} />
                  </div>
                </>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}