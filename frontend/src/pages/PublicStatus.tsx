// Status page pública — RF-019..024, RNF-009..010/015/018.
// Consome /status (rest) + SSE /events com snapshot/reconexão; grupos por
// seção; incidentes em aberto + timeline; gráficos por endpoint.
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { fmtDuration, fmtTime } from "../lib/api";
import { sse } from "../lib/sse";
import type { Incident, PublicEndpoint, SSEEvent } from "../lib/types";
import { useConfig, useIncidents, useStatus } from "../hooks/queries";
import { queryClient } from "../lib/query";
import { EndpointCard } from "../components/EndpointCard";
import { Spinner } from "../components/Spinner";

type EndpointMap = Map<number, PublicEndpoint>;

export default function PublicStatus() {
  const { data: cfg } = useConfig();
  const { data: status } = useStatus();
  const { data: incidentsData } = useIncidents();

  const [endpoints, setEndpoints] = useState<EndpointMap>(new Map());
  const [openIncidents, setOpenIncidents] = useState<Incident[]>([]);
  const [connected, setConnected] = useState(false);

  // Inicializa com o snapshot REST.
  useEffect(() => {
    if (!status) return;
    setEndpoints(new Map(status.endpoints.map((e) => [e.id, e])));
    setOpenIncidents(status.incidents_open);
  }, [status]);

  const timeline = useMemo(() => incidentsData?.items ?? [], [incidentsData]);

  // SSE: snapshot no (re)connect + eventos de transição (RF-021, RNF-010).
  useEffect(() => {
    sse.setStateHandler(setConnected);
    sse.connect();
    const us = sse.on("snapshot", (ev: SSEEvent) => {
      const eps = (ev.endpoints ?? []) as PublicEndpoint[];
      setEndpoints(new Map(eps.map((e) => [e.id, e])));
      setOpenIncidents((ev.incidents_open ?? []) as Incident[]);
    });
    const uc = sse.on("status_changed", (ev: SSEEvent) => {
      setEndpoints((prev) => {
        const next = new Map(prev);
        const cur = next.get(Number(ev.endpoint_id));
        if (cur) next.set(Number(ev.endpoint_id), { ...cur, status: ev.status as PublicEndpoint["status"] });
        return next;
      });
    });
    const oi = sse.on("incident_opened", (ev: SSEEvent) => {
      const inc: Incident = {
        id: Number(ev.incident_id),
        endpoint_id: Number(ev.endpoint_id),
        started_at: new Date().toISOString(),
        ended_at: null,
        duration_ms: null,
        resolution: null,
      };
      setOpenIncidents((prev) => [...prev.filter((i) => i.id !== inc.id), inc]);
      void queryClient.invalidateQueries({ queryKey: ["incidents"] });
    });
    const ci = sse.on("incident_closed", (ev: SSEEvent) => {
      setOpenIncidents((prev) => prev.filter((i) => i.id !== Number(ev.incident_id)));
      void queryClient.invalidateQueries({ queryKey: ["incidents"] });
    });
    const ru = sse.on("rollup_updated", () => {
      // Atualiza gráficos expandidos sem recarregar a página (RF-021).
      void queryClient.invalidateQueries({ queryKey: ["series"] });
      void queryClient.invalidateQueries({ queryKey: ["summary"] });
    });
    const er = sse.on("endpoint_removed", (ev: SSEEvent) => {
      setEndpoints((prev) => {
        const next = new Map(prev);
        next.delete(Number(ev.endpoint_id));
        return next;
      });
    });
    return () => {
      us(); uc(); oi(); ci(); ru(); er();
      sse.disconnect();
    };
  }, []);

  const brand = cfg?.branding;
  const primary = brand?.primary_color ?? "#2563eb";

  const grouped = useMemo(() => {
    const order: string[] = [];
    const byGroup = new Map<string, PublicEndpoint[]>();
    for (const ep of endpoints.values()) {
      const g = ep.group_name || "Sem grupo";
      if (!byGroup.has(g)) {
        byGroup.set(g, []);
        order.push(g);
      }
      byGroup.get(g)!.push(ep);
    }
    return order.map((g) => ({ group: g, items: byGroup.get(g)! }));
  }, [endpoints]);

  const degraded = [...endpoints.values()].some((e) => e.status === "down" || e.status === "degraded");
  const voice = degraded
    ? "Alguns serviços apresentam problemas."
    : endpoints.size > 0
      ? "Todos os sistemas operacionais."
      : "Nenhum serviço cadastrado.";

  return (
    <div className="min-h-screen bg-slate-950 text-slate-200">
      {/* Cabeçalho com marca customizada (RF-023). */}
      <header className="border-b border-white/10" style={{ backgroundColor: primary }}>
        <div className="mx-auto max-w-3xl px-4 py-6">
          <div className="flex items-center justify-between gap-3">
            <h1 className="text-xl font-bold text-white drop-shadow">{brand?.title ?? "Central de Monitoramento"}</h1>
            <a
              href="#incidentes"
              className="rounded-full bg-black/20 px-3 py-1 text-xs font-medium text-white hover:bg-black/30"
            >
              Incidentes
            </a>
          </div>
          <p className="mt-1 text-sm text-white/90">{brand?.description || "Status em tempo real das APIs monitoradas."}</p>
          <p className="mt-3 text-sm font-medium text-white">
            {voice}
            {!connected && <span className="ml-2 text-xs font-normal text-white/70">(reconectando…)</span>}
          </p>
        </div>
      </header>

      <main className="mx-auto max-w-3xl px-4 py-6">
        {/* Incidentes em aberto */}
        {openIncidents.length > 0 && (
          <section aria-labelledby="abertos" className="mb-6 rounded-xl border border-rose-500/30 bg-rose-500/10 p-4">
            <h2 id="abertos" className="text-sm font-semibold text-rose-300">
              Incidentes em aberto
            </h2>
            <ul className="mt-2 space-y-1 text-sm">
              {openIncidents.map((inc) => (
                <li key={inc.id} className="flex justify-between gap-3 text-slate-300">
                  <span>
                    <span className="text-rose-200">#{inc.id}</span> · endpoint #{inc.endpoint_id}
                  </span>
                  <span className="text-xs text-slate-400">desde {fmtTime(inc.started_at)}</span>
                </li>
              ))}
            </ul>
          </section>
        )}

        {/* Endpoints por grupo */}
        {grouped.length === 0 && (
          <div className="rounded-xl border border-white/10 bg-slate-900/60 p-6 text-sm text-slate-400">
            {status ? "Nenhum serviço cadastrado no monitor." : <Spinner />}
          </div>
        )}

        {grouped.map(({ group, items }) => (
          <section key={group} className="mb-6">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-widest text-slate-500">{group}</h2>
            <div className="space-y-2">
              {items.map((ep) => (
                <EndpointCard key={ep.id} ep={ep} />
              ))}
            </div>
          </section>
        ))}

        {/* Timeline de incidentes (RF-022). */}
        <section id="incidentes" className="scroll-mt-6 mb-10">
          <h2 className="mb-3 text-sm font-semibold text-slate-300">Histórico de incidentes</h2>
          {timeline.length === 0 ? (
            <p className="text-sm text-slate-500">Nenhum incidente registrado.</p>
          ) : (
            <ul className="divide-y divide-white/5 rounded-xl border border-white/10 bg-slate-900/60 text-sm">
              {timeline.slice(0, 30).map((inc) => (
                <li key={inc.id} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
                  <div>
                    <span className="font-medium text-slate-200">#{inc.id}</span>
                    <span className="text-slate-400"> · endpoint #{inc.endpoint_id}</span>
                  </div>
                  <div className="text-xs text-slate-400">
                    {fmtTime(inc.started_at)}
                    {inc.ended_at && <> → {fmtTime(inc.ended_at)}</>}
                    <span className="ml-2 rounded bg-white/5 px-1.5 py-0.5 text-slate-300">
                      {inc.ended_at ? fmtDuration(inc.duration_ms) : "em aberto"}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      </main>

      <footer className="border-t border-white/10 py-4 text-center text-xs text-slate-500">
        {status ? `${fmtTime(status.generated_at)} · ${endpoints.size} serviços` : ""}
        {" · "}
        <Link to="/admin/login" className="text-slate-400 hover:text-slate-200">
          Painel administrativo
        </Link>
      </footer>
    </div>
  );
}