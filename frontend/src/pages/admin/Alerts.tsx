// Alertas + Aparência — config dinâmica via /admin/settings (RF-023, T4.5).
import { useEffect, useState, type FormEvent } from "react";
import { useSaveSettings, useSettings } from "../../hooks/queries";
import type { AlertsSettings, Branding } from "../../lib/types";
import { Spinner } from "../../components/Spinner";

const COLORS = ["#2563eb", "#0ea5e9", "#10b981", "#f59e0b", "#8b5cf6", "#ef4444"];

export default function AlertsPage() {
  const { data, isLoading } = useSettings();
  const save = useSaveSettings();

  const [alerts, setAlerts] = useState<AlertsSettings>({
    enabled: false,
    webhook_url: "",
    to_email: "",
    suppression_seconds: 300,
  });
  const [branding, setBranding] = useState<Branding>({
    title: "Central de Monitoramento",
    description: "",
    primary_color: "#2563eb",
  });
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  useEffect(() => {
    if (!data) return;
    if (data.alerts) setAlerts(data.alerts);
    if (data.branding) setBranding(data.branding);
  }, [data]);

  if (isLoading) return <Spinner label="Carregando configurações…" />;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setMsg(null);
    try {
      await save.mutateAsync({ alerts, branding });
      setMsg({ ok: true, text: "Configuração salva e já aplicada (notifier usa esses valores)." });
    } catch (err) {
      setMsg({ ok: false, text: err instanceof Error ? err.message : "Falha ao salvar" });
    }
  }

  const field =
    "mt-1 w-full rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400";
  const label = "text-xs font-medium text-slate-400";

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold text-slate-100">Configuração</h1>

      <form onSubmit={submit} className="space-y-6">
        {/* Alertas (RF-026/027) — SMTP host/credenciais ficam em env (S-05). */}
        <section className="rounded-xl border border-white/10 bg-slate-900/60 p-4">
          <h2 className="text-sm font-semibold text-slate-200">Canais de alerta</h2>
          <p className="mt-1 text-xs text-slate-500">
            A conexão SMTP (host/usuário/senha) é definida por variáveis de ambiente; aqui você gerencia o destino do
            webhook, e-mail e a janela de supressão usados pelo notifier.
          </p>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <label className="col-span-2 flex items-center gap-2 text-sm text-slate-300">
              <input
                type="checkbox"
                checked={alerts.enabled}
                onChange={(e) => setAlerts({ ...alerts, enabled: e.target.checked })}
                className="accent-sky-400"
              />
              Alertas habilitados
            </label>
            <label className="block">
              <span className={label}>Webhook URL (Slack/Discord)</span>
              <input
                type="url"
                value={alerts.webhook_url}
                onChange={(e) => setAlerts({ ...alerts, webhook_url: e.target.value })}
                className={field}
                placeholder="https://hooks.slack.com/services/…"
              />
            </label>
            <label className="block">
              <span className={label}>E-mail de destino</span>
              <input
                type="email"
                value={alerts.to_email}
                onChange={(e) => setAlerts({ ...alerts, to_email: e.target.value })}
                className={field}
                placeholder="ops@empresa.com"
              />
            </label>
            <label className="block">
              <span className={label}>Janela de supressão (s)</span>
              <input
                type="number"
                min={0}
                max={86400}
                value={alerts.suppression_seconds}
                onChange={(e) => setAlerts({ ...alerts, suppression_seconds: Number(e.target.value) })}
                className={field}
              />
            </label>
          </div>
        </section>

        {/* Branding da status page (RF-023). */}
        <section className="rounded-xl border border-white/10 bg-slate-900/60 p-4">
          <h2 className="text-sm font-semibold text-slate-200">Aparência da status page</h2>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <label className="block">
              <span className={label}>Título</span>
              <input
                maxLength={80}
                value={branding.title}
                onChange={(e) => setBranding({ ...branding, title: e.target.value })}
                className={field}
              />
            </label>
            <label className="block">
              <span className={label}>Cor primária</span>
              <div className="mt-1 flex items-center gap-2">
                {COLORS.map((c) => (
                  <button
                    key={c}
                    type="button"
                    aria-label={`cor ${c}`}
                    onClick={() => setBranding({ ...branding, primary_color: c })}
                    className="h-7 w-7 rounded-full ring-2 ring-offset-2 ring-offset-slate-900 transition-transform hover:scale-110"
                    style={{ backgroundColor: c, ...(branding.primary_color === c ? { ringColor: "#38bdf8" } : {}) }}
                  />
                ))}
                <input
                  type="color"
                  value={branding.primary_color}
                  onChange={(e) => setBranding({ ...branding, primary_color: e.target.value })}
                  className="h-8 w-12 cursor-pointer rounded border border-white/10 bg-transparent"
                  aria-label="cor personalizada"
                />
              </div>
            </label>
            <label className="block sm:col-span-2">
              <span className={label}>Descrição</span>
              <textarea
                maxLength={300}
                rows={2}
                value={branding.description}
                onChange={(e) => setBranding({ ...branding, description: e.target.value })}
                className={field}
              />
            </label>
          </div>
        </section>

        {msg && (
          <p role="status" className={msg.ok ? "text-sm text-emerald-300" : "text-sm text-rose-300"}>
            {msg.text}
          </p>
        )}

        <button
          type="submit"
          disabled={save.isPending}
          className="rounded-lg bg-sky-500 px-4 py-2 text-sm font-semibold text-white hover:bg-sky-400 disabled:opacity-50"
        >
          {save.isPending ? "Salvando…" : "Salvar configuração"}
        </button>
      </form>
    </div>
  );
}