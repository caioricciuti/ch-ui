# syntax=docker/dockerfile:1.7

FROM oven/bun:1.2.23 AS ui-builder
WORKDIR /src/ui

COPY ui/package.json ui/bun.lock ./
RUN bun install --frozen-lockfile

COPY ui/ ./
ENV CHUI_VITE_MINIFY=true \
    CHUI_VITE_REPORT_COMPRESSED=false
RUN bun run build

FROM golang:1.24-alpine AS go-builder
WORKDIR /src

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui-builder /src/ui/dist ./ui/dist

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" -o /out/ch-ui .

FROM alpine:3.20 AS runtime
RUN addgroup -S chui && adduser -S -G chui chui \
    && apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/data \
    && chown -R chui:chui /app

WORKDIR /app
COPY --from=go-builder /out/ch-ui /usr/local/bin/ch-ui

ENV DATABASE_PATH=/app/data/ch-ui.db

EXPOSE 3488
VOLUME ["/app/data"]

USER chui
ENTRYPOINT ["ch-ui", "server"]
