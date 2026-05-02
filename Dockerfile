# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
ARG BUN_VERSION=1
ARG ALPINE_VERSION=3.20

FROM --platform=$BUILDPLATFORM oven/bun:${BUN_VERSION}-alpine AS frontend

WORKDIR /app/web

RUN apk add --no-cache nodejs

COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile

COPY web/ ./

RUN bun run build -- --mode prod

FROM --platform=$TARGETPLATFORM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

ARG BUILD_VERSION=dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /app/web/dist ./web/dist

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s -X main.BuildVersion=${BUILD_VERSION}" -o sigmo .

FROM alpine:${ALPINE_VERSION} AS runner

WORKDIR /app

COPY --from=builder /app/sigmo /app/sigmo
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN set -eux \
	&& apk add --no-cache ca-certificates dbus libmbim-tools modemmanager qmi-utils \
	&& mkdir -p /run/dbus \
	&& chmod +x /usr/local/bin/docker-entrypoint.sh

ENV DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
