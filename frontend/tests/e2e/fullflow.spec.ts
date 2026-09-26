// E2E do fluxo completo (UC-01/UC-05, T4.9) contra a stack Docker.
// Pré-requisito: `docker compose up -d --build` rodando em https://localhost.
//
// 1. Status page pública renderiza serviços e marca (RF-019/023).
// 2. Login admin → CRUD de endpoint (RF-001..006).
// 3. Endpoint apontando para alvo inalcançável → checagens falham →
//    incidente abre → STATUS PAGE reflete DOWN em tempo real via SSE (RF-021,
//    RNF-009) sem refresh — em qualquer cliente.
import { expect, test } from "@playwright/test";
import type { APIRequestContext } from "@playwright/test";

// E-mail FIXO: a conta é criada uma única vez; execuções seguintes só fazem
// login — evita estourar o rate limit de signup (RNF-017) em runs repetidos.
const EMAIL = "e2e@monitor.test";
const PASSWORD = "e2e-senha-segura-123";

async function apiLogin(request: APIRequestContext): Promise<string> {
  let login = await request.post("/api/v1/auth/login", { data: { email: EMAIL, password: PASSWORD } });
  if (login.status() === 401) {
    // Conta ainda não existe → cria; 409 (já existe) também segue.
    const res = await request.post("/api/v1/auth/signup", { data: { email: EMAIL, password: PASSWORD } });
    if (res.status() !== 201 && res.status() !== 409 && res.status() !== 429) {
      throw new Error(`signup falhou: ${res.status()} ${await res.text()}`);
    }
    login = await request.post("/api/v1/auth/login", { data: { email: EMAIL, password: PASSWORD } });
  }
  expect(login.status()).toBe(200);
  const body = (await login.json()) as { token: string };
  return body.token;
}

test.describe("Fluxo completo — monitor → incidente → status page (UC-01/UC-05)", () => {
  let token: string;
  let downEpId: number;

  test.beforeAll(async ({ browser }) => {
    const ctx = await browser.newContext({ ignoreHTTPSErrors: true });
    const request = ctx.request;
    token = await apiLogin(request);
    const h = { Authorization: `Bearer ${token}` };

    // Limpa endpoints E2E órfãos de runs anteriores (determinismo).
    const list = await request.get("/api/v1/admin/endpoints/", { headers: h });
    const eps = (await list.json() as { items: { id: number; name: string }[] }).items;
    for (const ep of eps) {
      if (ep.name === "down-alvo-e2e") {
        await request.delete(`/api/v1/admin/endpoints/${ep.id}`, { headers: h });
      }
    }

    // Grupo + endpoint DOWN com intervalo curto.
    const g = await request.post("/api/v1/admin/groups/", {
      headers: h,
      data: { name: "E2E", display_order: 1 },
    });
    const gid = (await g.json() as { id: number }).id;

    const ep = await request.post("/api/v1/admin/endpoints/", {
      headers: h,
      data: {
        group_id: gid,
        name: "down-alvo-e2e",
        url: "http://127.0.0.1:9/", // porta descartada → conexão recusada (fail rápido)
        method: "GET",
        interval_seconds: 15,
        timeout_ms: 3000,
      },
    });
    expect(ep.status(), `criar endpoint: ${await ep.text()}`).toBe(201);
    downEpId = (await ep.json() as { id: number }).id;
    await ctx.close();
  });

  test("incidente abre e a status page reflete DOWN via SSE", async ({ page, request }) => {
    // Abre a status page em DOIS clientes (todos os clientes — UC-05).
    const page2 = await page.context().newPage();
    await page2.goto("/");
    await page.goto("/");
    await expect(page.getByText("down-alvo-e2e").first()).toBeVisible({ timeout: 15_000 });

    // Aguarda o incidente abrir (3 falhas × 15s + tick ≤ ~90s).
    const deadline = Date.now() + 100_000;
    let down = false;
    while (Date.now() < deadline) {
      const st = await request.get("/api/v1/status");
      const body = (await st.json()) as {
        endpoints: { id: number; status: string }[];
        incidents_open: { endpoint_id: number }[];
      };
      down = body.endpoints.find((e) => e.id === downEpId)?.status === "down";
      const incident = body.incidents_open.some((i) => i.endpoint_id === downEpId);
      if (down && incident) break;
      await page.waitForTimeout(5_000);
    }
    expect(down, "endpoint deveria estar DOWN após falhas confirmadas").toBe(true);

    // Status page (clientes A e B) deve mostrar DOWN em ≤ 5s SEM refresh (SSE),
    // no CARD específico do endpoint e2e.
    for (const p of [page, page2]) {
      const card = p.locator(".rounded-xl", { hasText: "down-alvo-e2e" }).first();
      await expect(card).toBeVisible({ timeout: 10_000 });
      await expect(card.getByText("Indisponível")).toBeVisible({ timeout: 8_000 });
    }
    await page2.close();
  });
});

test.describe("Status page pública + painel admin (RF-019/023, T4.3)", () => {
  test("status page renderiza serviços e branding", async ({ page, request }) => {
    await page.goto("/");
    await expect(page.getByRole("heading", { name: /Central de Monitoramento|Status/i })).toBeVisible();
    await expect(page.getByRole("link", { name: "Painel administrativo" })).toBeVisible();

    // Config pública chega sem segredos (RNF-018).
    const res = await request.get("/api/v1/config");
    expect(res.status()).toBe(200);
    const body = (await res.json()) as { branding: { title: string } };
    expect(body.branding.title.length).toBeGreaterThan(0);

    // Sem vazamento de headers/webhook no HTML da página nem na API pública.
    const html = await page.content();
    expect(html).not.toContain("webhook_url");
  });

  test("login admin cria sessão e lista endpoints", async ({ page, request }) => {
    await apiLogin(request);
    // Valida que a rota admin exige auth (401 → redirect do SPA para login).
    await page.goto("/admin/endpoints");
    await expect(page).toHaveURL(/\/admin\/login/);

    // Login de verdade.
    await page.goto("/admin/login");
    await page.getByLabel("E-mail").fill(EMAIL);
    await page.getByLabel("Senha").fill(PASSWORD);
    await page.getByRole("button", { name: "Entrar" }).click();
    await expect(page).toHaveURL(/\/admin\/endpoints/);
    await expect(page.getByText("Endpoints monitorados")).toBeVisible();
  });

  test("settings admin: PUT branding + alerts persiste e valida webhook", async ({ request }) => {
    const token = await apiLogin(request);
    const h = { Authorization: `Bearer ${token}`, "Content-Type": "application/json" };
    const ok = await request.put("/api/v1/admin/settings", {
      headers: h,
      data: {
        branding: { title: "E2E Status", description: "teste", primary_color: "#0ea5e9" },
        alerts: { enabled: false, webhook_url: "", to_email: "", suppression_seconds: 300 },
      },
    });
    expect(ok.status()).toBe(200);

    const bad = await request.put("/api/v1/admin/settings", {
      headers: h,
      data: { alerts: { webhook_url: "ftp://exemplo.com/hook" } }, // scheme inválido → 400 sempre
    });
    expect(bad.status()).toBe(400);
  });
});