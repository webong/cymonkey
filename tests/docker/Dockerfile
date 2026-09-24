# Test-only Linux engine environment. This is not product topology.
FROM golang:1.26-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY src ./src
COPY lib ./lib
COPY cmd ./cmd
RUN mkdir -p /out \
  && go build -o /out/cymonkey ./src

FROM node:22-bookworm

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    chromium \
    curl \
    firefox-esr \
    fonts-liberation \
    webkit2gtk-driver \
    xvfb \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY tests ./tests
COPY examples ./examples
COPY src/internal/bridge ./src/internal/bridge
COPY scripts ./scripts
COPY --from=builder /out/cymonkey /app/bin/cymonkey
RUN chmod +x tests/docker/*.sh scripts/browser-worker.mjs

ENTRYPOINT ["tests/docker/entrypoint.sh"]
