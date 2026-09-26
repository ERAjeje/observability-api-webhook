#!/usr/bin/env bash
# Deploy na VPS (checklist final — RNF-003/RNF-016).
#
# Uso (como usuário com sudo):
#   DOMAIN=status.exemplo.com EMAIL=ops@exemplo.com bash deploy/provision.sh
#
# O que faz:
#   1. Instala Docker + plugin compose (Ubuntu/Debian);
#   2. Sobe o repo (clone/rsync) e gera .env com segredos aleatórios;
#   3. Compila o frontend (estáticos) e sobe a stack (postgres+backend+nginx);
#   4. Emite certificado TLS real via acme.sh e instala em deploy/nginx/certs
#      com os NOMES que o nginx espera (fullchain.pem + privkey.pem).
#
# Variavéis:
#   DOMAIN        obrigatório — FQDN da status page
#   EMAIL         obrigatório — contato ACME
#   REPO_URL      origem do código (default: copia o diretório atual)
#   STACK_DIR     diretório de instalação (default: /opt/monitor)
set -euo pipefail

DOMAIN="${DOMAIN:-}"
EMAIL="${EMAIL:-}"
STACK_DIR="${STACK_DIR:-/opt/monitor}"
REPO_URL="${REPO_URL:-}"

[ -n "$DOMAIN" ] || { echo "erro: defina DOMAIN"; exit 1; }
[ -n "$EMAIL" ] || { echo "erro: defina EMAIL"; exit 1; }

log() { printf '\n\033[1;32m==> %s\033[0m\n' "$*"; }

log "1/6 — Docker + compose plugin"
if ! command -v docker >/dev/null 2>&1; then
  curl -fsSL https://get.docker.com | sh
fi
docker compose version >/dev/null 2>&1 \
  || { echo "instale o plugin compose: apt install docker-compose-plugin"; exit 1; }

log "2/6 — Código em $STACK_DIR"
sudo mkdir -p "$STACK_DIR"
sudo chown "$(id -u):$(id -g)" "$STACK_DIR"
if [ -n "$REPO_URL" ]; then
  git clone "$REPO_URL" "$STACK_DIR" 2>/dev/null || (cd "$STACK_DIR" && git pull --ff-only)
  cd "$STACK_DIR"
else
  echo "  (modo local: copiando o diretório atual)"
  cp -r "$PWD"/. "$STACK_DIR"/
  cd "$STACK_DIR"
fi

log "3/6 — Segredos (.env)"
if [ ! -f .env ]; then
  cp .env.example .env
  sed -i "s|^DB_PASSWORD=.*|DB_PASSWORD=$(openssl rand -hex 24)|" .env
  sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -hex 32)|" .env
  sed -i "s|^ENV=.*|ENV=production|" .env
  echo "  .env gerado com DB_PASSWORD e JWT_SECRET aleatórios"
else
  echo "  .env já existe — mantido"
fi

log "4/6 — Build do frontend + stack"
if command -v node >/dev/null 2>&1; then
  make frontend-build
else
  echo "  (node não instalado no host — usando dist commitado no repo)"
fi
docker compose up -d --build

log "5/6 — Guarda readyz"
for i in $(seq 1 40); do
  curl -sfk "https://localhost/readyz" >/dev/null 2>&1 && break
  sleep 3
done
curl -sfk "https://localhost/readyz" >/dev/null 2>&1 \
  || { echo "falha: backend não ficou ready"; docker compose ps; exit 1; }

log "6/6 — TLS real (acme.sh)"
CERT_DIR="$STACK_DIR/deploy/nginx/certs"
mkdir -p "$CERT_DIR"
if ! command -v acme.sh >/dev/null 2>&1; then
  curl https://get.acme.sh | sh -s email="$EMAIL"
fi
export PATH="$HOME/.acme.sh:$PATH"
if [ -s "$CERT_DIR/fullchain.pem" ]; then
  echo "  certificado já instalado — renovando…"
  acme.sh --renew -d "$DOMAIN" --server letsencrypt || true
else
  # Emissão standalone (exige porta 80 livre por alguns segundos).
  acme.sh --issue --standalone -d "$DOMAIN" --server letsencrypt
fi
acme.sh --install-cert -d "$DOMAIN" \
  --key-file "$CERT_DIR/privkey.pem" \
  --fullchain-file "$CERT_DIR/fullchain.pem" \
  --reloadcmd "cd $STACK_DIR && docker compose restart nginx" \
  || { echo "falha ao instalar certificado"; exit 1; }

log "✔ Deploy concluído — https://$DOMAIN"
echo "Status page:   https://$DOMAIN"
echo "Painel admin:  https://$DOMAIN/admin/login"
echo "Métricas:      https://$DOMAIN/metrics   (proteja por firewall — S-11)"