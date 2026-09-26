// Gerenciador único de SSE (RF-021, RNF-010): EventSource com reconexão
// automática nativa; páginas registram listeners por tipo de evento.
import type { SSEEvent } from "./types";

type Listener = (ev: SSEEvent) => void;

class SSEStream {
  private es: EventSource | null = null;
  private listeners = new Map<string, Set<Listener>>();
  private onState: ((connected: boolean) => void) | null = null;

  /** Registra um listener para um tipo de evento (ou para "snapshot"). */
  on(type: string, fn: Listener): () => void {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(fn);
    return () => this.off(type, fn);
  }

  off(type: string, fn: Listener): void {
    this.listeners.get(type)?.delete(fn);
  }

  setStateHandler(fn: (connected: boolean) => void): void {
    this.onState = fn;
  }

  /** Abre o stream (idempotente). Chamado pelo app no boot. */
  connect(): void {
    if (this.es) return;
    const es = new EventSource("/api/v1/events");
    this.es = es;
    es.onopen = () => this.onState?.(true);
    es.onerror = () => {
      // EventSource reconecta automaticamente; só reportamos queda.
      this.onState?.(false);
    };
    for (const name of ["snapshot", "status_changed", "incident_opened", "incident_closed", "rollup_updated", "endpoint_removed"]) {
      es.addEventListener(name, (e) => {
        const raw = (e as MessageEvent).data;
        try {
          this.dispatch(name, JSON.parse(raw));
        } catch {
          // frames não-JSON (ex.: ping) são ignorados
        }
      });
    }
  }

  disconnect(): void {
    this.es?.close();
    this.es = null;
  }

  private dispatch(type: string, data: Record<string, unknown>): void {
    const ev: SSEEvent = { type, ...data };
    this.listeners.get(type)?.forEach((fn) => fn(ev));
    this.listeners.get("*")?.forEach((fn) => fn(ev));
  }
}

export const sse = new SSEStream();