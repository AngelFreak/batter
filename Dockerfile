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

# Stage 4: Production image
FROM alpine:3.20
RUN apk add --no-cache ca-certificates android-tools nodejs

WORKDIR /app

# Copy Go binary
COPY --from=go-builder /batter /app/batter

# Copy Next.js standalone output
COPY --from=web-builder /app/.next/standalone /app/web/
COPY --from=web-builder /app/.next/static /app/web/.next/static
COPY --from=web-builder /app/public /app/web/public

# Copy scrcpy-server
COPY --from=scrcpy-downloader /scrcpy-server /usr/local/share/scrcpy/scrcpy-server

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
