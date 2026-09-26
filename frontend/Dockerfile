# Frontend — multi-stage (arquitetura §6.3): build Node → estáticos puros.
# Em produção o Nginx edge serve esses estáticos (ver docker-compose.yml —
# o build roda na CI/host e o dist é montado no Nginx; Node jamais roda em runtime).
FROM node:22-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# Stage estático — apenas os assets (o proxy/edge é o nginx do compose).
FROM scratch AS static
COPY --from=build /app/dist /dist