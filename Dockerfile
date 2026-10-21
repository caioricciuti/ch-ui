# syntax=docker/dockerfile:1.7

# Base images are pinned by digest. Bun matches ui/.bun-version, Go matches
# the go directive in go.mod.
FROM oven/bun:1.4.2@sha256:9114c058aeae42162ee16dd5084b95fe9473970bb6bcb5b232ab1630f0546895 AS ui-builder
WORKDIR /src/ui

COPY ui/package.json ui/bun.lock ./
RUN bun install --frozen-lockfile

COPY ui/ ./
ENV CHUI_VITE_MINIFY=true \
    CHUI_VITE_REPORT_COMPRESSED=false
RUN bun run build

FROM golang:1.26.10-alpine3.24@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS go-builder
WORKDIR /src

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui-builder /src/ui/dist ./ui/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" -o /out/ch-ui .

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runtime
RUN addgroup -S chui && adduser -S -G chui chui \
    && apk add --no-cache ca-certificates tzdata wget \
    && mkdir -p /app/data \
    && chown -R chui:chui /app

WORKDIR /app
COPY --from=go-builder /out/ch-ui /usr/local/bin/ch-ui

ENV DATABASE_PATH=/app/data/ch-ui.db

EXPOSE 3488
VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:3488/health || exit 1

USER chui
ENTRYPOINT ["ch-ui", "server"]
