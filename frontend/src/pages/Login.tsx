import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, setToken } from "../lib/api";

export default function Login() {
  const nav = useNavigate();
  const [params] = useSearchParams();
  const [mode, setMode] = useState<"login" | "signup">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      if (mode === "signup") {
        await api.signup({ email, password });
        setMode("login");
        setPassword("");
        setError("Conta criada — entre para continuar.");
      } else {
        const { token } = await api.login({ email, password });
        setToken(token);
        nav(params.get("next") && params.get("next")!.startsWith("/admin") ? params.get("next")! : "/admin/endpoints", {
          replace: true,
        });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Falha inesperada");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-950 px-4">
      <div className="w-full max-w-sm">
        <h1 className="mb-1 text-center text-xl font-bold text-slate-100">Central de Monitoramento</h1>
        <p className="mb-6 text-center text-sm text-slate-500">Acesso administrativo</p>
        <form onSubmit={submit} className="space-y-4 rounded-xl border border-white/10 bg-slate-900 p-6">
          <label className="block">
            <span className="text-xs font-medium text-slate-400">E-mail</span>
            <input
              type="email"
              required
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="mt-1 w-full rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
            />
          </label>
          <label className="block">
            <span className="text-xs font-medium text-slate-400">Senha</span>
            <input
              type="password"
              required
              minLength={8}
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="mt-1 w-full rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
            />
          </label>
          {error && (
            <p role="alert" className="text-xs text-slate-300">
              {error}
            </p>
          )}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded-lg bg-sky-500 px-3 py-2 text-sm font-semibold text-white hover:bg-sky-400 disabled:opacity-50"
          >
            {mode === "login" ? "Entrar" : "Criar conta"}
          </button>
          <button
            type="button"
            onClick={() => setMode((m) => (m === "login" ? "signup" : "login"))}
            className="w-full text-center text-xs text-slate-400 hover:text-slate-200"
          >
            {mode === "login" ? "Criar uma conta" : "Já tenho conta"}
          </button>
        </form>
      </div>
    </div>
  );
}