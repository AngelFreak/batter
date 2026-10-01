# Stage 1: Build Go backend
FROM golang:1.24-alpine AS go-builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /batter ./cmd/batter

# Stage 2: Build Next.js frontend
FROM node:20-alpine AS web-builder
WORKDIR /app
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# Stage 3: Download scrcpy-server
FROM alpine:3.20 AS scrcpy-downloader
ARG SCRCPY_VERSION=3.3.4
RUN apk add --no-cache wget
RUN wget -q -O /scrcpy-server \
    "https://github.com/Genymobile/scrcpy/releases/download/v${SCRCPY_VERSION}/scrcpy-server-v${SCRCPY_VERSION}"

# Stage 4: gnirehtet (reverse tethering). The relay is built from source
# because the release binary is glibc-only; libc is bumped because v2.5.1's
# pinned libc crate links open64, which current musl no longer provides.
FROM rust:1-alpine AS gnirehtet-builder
ARG GNIREHTET_VERSION=2.5.1
RUN apk add --no-cache musl-dev git
RUN git clone -q --depth 1 --branch v${GNIREHTET_VERSION} https://github.com/Genymobile/gnirehtet /src
WORKDIR /src/relay-rust
RUN cargo update -q -p libc --precise 0.2.189 && cargo build --release

FROM alpine:3.20 AS gnirehtet-apk
ARG GNIREHTET_VERSION=2.5.1
ARG GNIREHTET_ZIP_SHA256=dee55499ca4fef00ce2559c767d2d8130163736d43fdbce753e923e75309c275
RUN apk add --no-cache wget unzip
RUN wget -q -O /g.zip "https://github.com/Genymobile/gnirehtet/releases/download/v${GNIREHTET_VERSION}/gnirehtet-rust-linux64-v${GNIREHTET_VERSION}.zip" \
    && echo "${GNIREHTET_ZIP_SHA256}  /g.zip" | sha256sum -c - \
    && unzip -q -j /g.zip '*/gnirehtet.apk' -d /out

# Stage 5: Production image
FROM alpine:3.20
# iproute2 + wg: WireGuard tunnel and policy routing for tethered devices.
RUN apk add --no-cache ca-certificates android-tools nodejs iproute2 wireguard-tools-wg nftables

WORKDIR /app

# Copy Go binary
COPY --from=go-builder /batter /app/batter

# Copy Next.js standalone output
COPY --from=web-builder /app/.next/standalone /app/web/
COPY --from=web-builder /app/.next/static /app/web/.next/static
COPY --from=web-builder /app/public /app/web/public

# Copy scrcpy-server
COPY --from=scrcpy-downloader /scrcpy-server /usr/local/share/scrcpy/scrcpy-server

# Copy gnirehtet relay + device app
COPY --from=gnirehtet-builder /src/relay-rust/target/release/gnirehtet /usr/local/bin/gnirehtet
COPY --from=gnirehtet-apk /out/gnirehtet.apk /usr/local/share/gnirehtet/gnirehtet.apk

# Create data directory
RUN mkdir -p /app/data

EXPOSE 3000 8080

# Start both backend and frontend
COPY <<'EOF' /app/start.sh
#!/bin/sh
set -e

# Start Next.js frontend on port 3000
cd /app/web && PORT=3000 HOSTNAME=0.0.0.0 node server.js &

# Start Go backend
exec /app/batter
EOF
RUN chmod +x /app/start.sh

CMD ["/app/start.sh"]
