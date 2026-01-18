# UserEngine Dockerfile
# Build with: docker build --build-arg SERVICE=gateway -t userengine-gateway .

# ============================================
# Build Stage
# ============================================
FROM golang:1.21-alpine AS builder

ARG SERVICE=gateway

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Download dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the service
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /service ./cmd/${SERVICE}

# ============================================
# Runtime Stage
# ============================================
FROM alpine:3.19

# Install CA certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 userengine && \
    adduser -u 1000 -G userengine -s /bin/sh -D userengine

WORKDIR /app

# Copy binary from builder
COPY --from=builder /service /app/service

# Copy Lua scripts
COPY --from=builder /app/pkg/presence/lua /app/lua

# Set ownership
RUN chown -R userengine:userengine /app

# Switch to non-root user
USER userengine

# Environment defaults
ENV REDIS_ADDR=localhost:6379 \
    JWT_SECRET=change-me-in-production \
    LUA_SCRIPTS_PATH=/app/lua

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/service"]

