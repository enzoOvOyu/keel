FROM node:22.23.3-bookworm-slim AS web
WORKDIR /src
COPY package.json package-lock.json ./
COPY web/package.json ./web/package.json
RUN npm ci
COPY web ./web
COPY api ./api
RUN npm run build

FROM golang:1.27.2-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY ent ./ent
ARG APP_VERSION=development
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${APP_VERSION}" -o /keel-api ./cmd/keel-api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /keel-admin ./cmd/keel-admin \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /keel-worker ./cmd/keel-worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /keel-sim ./cmd/keel-sim \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /keel-ssh-runner ./cmd/keel-ssh-runner \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${APP_VERSION}" -o /keel-agent ./cmd/keel-agent

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend /keel-api /app/keel-api
COPY --from=backend /keel-admin /app/keel-admin
COPY --from=backend /keel-worker /app/keel-worker
COPY --from=backend /keel-sim /app/keel-sim
COPY --from=backend /keel-ssh-runner /app/keel-ssh-runner
COPY --from=backend /keel-agent /app/keel-agent
COPY --from=web /src/web/dist /app/web/dist
ENV KEEL_LISTEN_ADDR=0.0.0.0:3000 KEEL_STATIC_DIR=/app/web/dist
USER 65532:65532
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 CMD curl --fail --silent http://127.0.0.1:3000/healthz || exit 1
CMD ["/app/keel-api"]
