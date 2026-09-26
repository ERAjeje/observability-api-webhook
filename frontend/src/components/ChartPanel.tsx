// Painel de gráficos de um endpoint (carregado sob demanda — Recharts fora
// do bundle inicial da status page — RNF-015/Lighthouse ≥ 90).
import { useMemo, useState } from "react";
import clsx from "clsx";
import type { PublicEndpoint } from "../lib/types";
import { useSeries } from "../hooks/queries";
import { LatencyChart, UptimeChart } from "./charts";
import { Spinner } from "./Spinner";

type Period = "24h" | "7d" | "30d";
const PERIODS: { key: Period; hours: number; bucket: "minute" | "hour" | "day" }[] = [
  { key: "24h", hours: 24, bucket: "minute" },
  { key: "7d", hours: 24 * 7, bucket: "hour" },
  { key: "30d", hours: 24 * 30, bucket: "day" },
];

function isoHoursAgo(h: number): string {
  return new Date(Date.now() - h * 3600_000).toISOString();
}

export function ChartPanel({ ep }: { ep: PublicEndpoint }) {
  const [period, setPeriod] = useState<Period>("24h");
  const p = PERIODS.find((x) => x.key === period)!;
  const series = useSeries(ep.id, isoHoursAgo(p.hours), new Date().toISOString(), p.bucket);

  const rows = useMemo(() => (series.data?.items ?? []).filter((x) => x.count > 0), [series.data]);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-1" role="tablist" aria-label="Período do gráfico">
        {PERIODS.map(({ key }) => (
          <button
            key={key}
            role="tab"
            aria-selected={period === key}
            onClick={() => setPeriod(key)}
            className={clsx(
              "rounded-lg px-2.5 py-1 text-xs font-medium transition-colors",
              period === key ? "bg-sky-500/20 text-sky-300 ring-1 ring-sky-400/40" : "text-slate-400 hover:text-slate-200",
            )}
          >
            {key}
          </button>
        ))}
      </div>
      {series.isFetching ? (
        <Spinner label="Carregando gráficos…" />
      ) : rows.length === 0 ? (
        <p className="text-xs text-slate-500">Sem dados de rollups neste período.</p>
      ) : (
        <>
          <div>
            <p className="mb-1 text-xs uppercase tracking-wide text-slate-500">Latência</p>
            <LatencyChart data={series.data!.items} />
          </div>
          <div>
            <p className="mb-1 text-xs uppercase tracking-wide text-slate-500">Uptime</p>
            <UptimeChart data={series.data!.items} />
          </div>
        </>
      )}
    </div>
  );
}