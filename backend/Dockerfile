# syntax=docker/dockerfile:1
# Multi-stage: build estático → runtime distroless (arquitetura §6.2, T1.6).
# Migrations vão embarcadas no binário via go:embed (não precisam de COPY).

# ── STAGE 1: build ────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS build
WORKDIR /src

# Cache de camadas: baixa dependências antes do código.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /app/monitor ./cmd/monitor

# ── STAGE 2: runtime ──────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app/monitor /monitor
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/monitor"]