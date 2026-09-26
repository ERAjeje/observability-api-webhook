// Gráficos (Recharts) da status page — renderizam dados agregados (RNF-014).
import { useMemo } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { SeriesPoint } from "../lib/types";

const axis = { stroke: "#64748b", fontSize: 11 } as const;
const tooltipStyle = {
  backgroundColor: "#0f172a",
  border: "1px solid #334155",
  borderRadius: 8,
  fontSize: 12,
  color: "#e2e8f0",
};

export function LatencyChart({ data, height = 180 }: { data: SeriesPoint[]; height?: number }) {
  const rows = useMemo(
    () =>
      data.map((p) => ({
        t: new Date(p.bucket).toLocaleString("pt-BR", {
          day: "2-digit",
          month: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        }),
        p50: p.p50_ms,
        p95: p.p95_ms,
        avg: p.avg_ms,
      })),
    [data],
  );
  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={rows} margin={{ top: 4, right: 12, bottom: 0, left: 0 }}>
        <CartesianGrid stroke="#1e293b" vertical={false} />
        <XAxis dataKey="t" {...axis} tickMargin={6} minTickGap={40} />
        <YAxis {...axis} tickFormatter={(v: number) => `${v}ms`} width={56} />
        <Tooltip contentStyle={tooltipStyle} />
        <Line type="monotone" dataKey="p95" name="P95" stroke="#f59e0b" strokeWidth={2} dot={false} />
        <Line type="monotone" dataKey="p50" name="P50" stroke="#38bdf8" strokeWidth={2} dot={false} />
        <Line type="monotone" dataKey="avg" name="Média" stroke="#94a3b8" strokeWidth={1.5} dot={false} />
      </LineChart>
    </ResponsiveContainer>
  );
}

export function UptimeChart({ data, height = 140 }: { data: SeriesPoint[]; height?: number }) {
  const rows = useMemo(
    () =>
      data.map((p) => ({
        t: new Date(p.bucket).toLocaleString("pt-BR", {
          day: "2-digit",
          month: "2-digit",
          hour: "2-digit",
        }),
        uptime: p.uptime_pct,
      })),
    [data],
  );
  return (
    <ResponsiveContainer width="100%" height={height}>
      <AreaChart data={rows} margin={{ top: 4, right: 12, bottom: 0, left: 0 }}>
        <defs>
          <linearGradient id="uptimeFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity={0.45} />
            <stop offset="100%" stopColor="#10b981" stopOpacity={0.05} />
          </linearGradient>
        </defs>
        <CartesianGrid stroke="#1e293b" vertical={false} />
        <XAxis dataKey="t" {...axis} tickMargin={6} minTickGap={40} />
        <YAxis domain={[0, 100]} {...axis} tickFormatter={(v: number) => `${v}%`} width={44} />
        <Tooltip contentStyle={tooltipStyle} formatter={(v: number) => [`${v}%`, "Uptime"]} />
        <Area type="monotone" dataKey="uptime" stroke="#10b981" strokeWidth={2} fill="url(#uptimeFill)" />
      </AreaChart>
    </ResponsiveContainer>
  );
}