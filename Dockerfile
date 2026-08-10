# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.3
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
ARG BUILD_COMMIT=

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /app/web/dist ./web/dist

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Version=${BUILD_VERSION} \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Commit=${BUILD_COMMIT} \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Channel=stable \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Edition=community \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Distribution=container" -o /app/sigmo .

FROM alpine:${ALPINE_VERSION} AS runner

WORKDIR /app

COPY --from=builder /app/sigmo /app/sigmo

RUN apk add --no-cache ca-certificates

ENTRYPOINT ["/app/sigmo"]
