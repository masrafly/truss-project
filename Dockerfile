# syntax=docker/dockerfile:1

# Image produksi TruSS API: build multi-stage menjadi satu binary statis di image
# distroless (tanpa shell, tanpa package manager) yang berjalan sebagai user non-root.
#
# Build  : docker build -t truss-api .
# Jalan  : docker run --rm -p 8080:8080 --env-file .env -e DB_HOST=... truss-api
# Konfigurasi HANYA lewat environment variable (lihat .env.example); .env tidak
# dimasukkan ke image (lihat .dockerignore). Skema database tidak dibuat oleh image ini:
# jalankan migration (folder migrations/) dengan golang-migrate sebelum aplikasi.

# Harus sama dengan baris "go" di go.mod (versi minimal yang memuat perbaikan keamanan).
ARG GO_VERSION=1.27.4

# ---------- Tahap 1: build ----------
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# CGO mati: binary statis, tidak bergantung pada libc image runtime.
# GOTOOLCHAIN=local: gagal jika Go image tidak memenuhi go.mod, alih-alih diam-diam
# mengunduh toolchain lain saat build.
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local

# Dependensi dulu supaya layer ini ter-cache selama go.mod/go.sum tidak berubah.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal

# -trimpath: path build lokal tidak tertanam di binary. -s -w: buang tabel simbol dan debug.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---------- Tahap 2: runtime ----------
# static-debian12:nonroot = sertifikat CA + tzdata + user "nonroot" (uid 65532), tanpa shell.
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="truss-api" \
      org.opencontainers.image.source="https://github.com/masrafly/truss-project"

COPY --from=build /out/api /app/api

# Production: log JSON dan Gin release mode. Dapat ditimpa dengan -e APP_ENV=development.
ENV APP_ENV=production \
    APP_PORT=8080

USER nonroot:nonroot
EXPOSE 8080

# Distroless tidak punya curl; binary sendiri yang menjadi klien HTTP-nya (GET /healthz).
# /healthz tidak menyentuh database, jadi gangguan DB tidak membuat container di-restart.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/app/api", "healthcheck"]

ENTRYPOINT ["/app/api"]
