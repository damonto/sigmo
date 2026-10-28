# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.27.3
ARG BUN_VERSION=1
ARG ALPINE_VERSION=3.20

FROM --platform=$BUILDPLATFORM oven/bun:${BUN_VERSION}-alpine AS frontend

WORKDIR /app/apps/web

RUN apk add --no-cache nodejs

COPY apps/web/package.json apps/web/bun.lock ./
RUN bun install --frozen-lockfile

COPY apps/web/ ./
COPY internal/pkg/apierror/ /app/internal/pkg/apierror/

RUN bun run build -- --mode prod

FROM --platform=$TARGETPLATFORM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=
ARG SIGMO_WORKER_URL=https://sigmo-pro.952007.xyz
ARG SIGMO_LICENSE_PUBLIC_KEY=

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /app/apps/web/dist ./apps/web/dist

RUN purego_dir="$(go list -m -f '{{.Dir}}' github.com/ebitengine/purego)" \
    && cp -R "${purego_dir}" /tmp/sigmo-purego \
    && go mod edit -replace=github.com/ebitengine/purego=/tmp/sigmo-purego \
    && scripts/patch-purego-musl.sh

RUN case "$(go env GOARCH)" in \
      amd64) interpreter=/lib/ld-musl-x86_64.so.1 ;; \
      arm64) interpreter=/lib/ld-musl-aarch64.so.1 ;; \
      *) echo "unsupported container architecture" >&2; exit 1 ;; \
    esac \
    && license_flags="" \
    && if [ -n "${SIGMO_LICENSE_PUBLIC_KEY}" ]; then license_flags="-X github.com/damonto/sigmo/internal/pkg/license.LicensePublicKey=${SIGMO_LICENSE_PUBLIC_KEY}"; fi \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-I ${interpreter} -w -s \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Version=${BUILD_VERSION} \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Commit=${BUILD_COMMIT} \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Channel=stable \
    -X github.com/damonto/sigmo/internal/app/buildinfo.Distribution=container \
    -X github.com/damonto/sigmo/internal/pkg/license.WorkerURL=${SIGMO_WORKER_URL:-https://sigmo-pro.952007.xyz} \
    ${license_flags}" -o /app/sigmo .

FROM alpine:${ALPINE_VERSION} AS runner

WORKDIR /app

COPY --from=builder /app/sigmo /app/sigmo

RUN apk add --no-cache ca-certificates pcsc-lite-libs

ENTRYPOINT ["/app/sigmo"]
