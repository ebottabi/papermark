# syntax=docker/dockerfile:1.7

###############################################################################
# Stage 1 — Build: static, stripped linux binaries.
###############################################################################
FROM golang:1.26-alpine AS backend

WORKDIR /app
RUN apk add --no-cache git ca-certificates

# Module cache layer
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/worker ./cmd/worker

###############################################################################
# Stage 2 — Runtime. The agent's shell_exec runs /bin/sh in task workspaces,
# so the image keeps busybox and the common tools agents reach for.
###############################################################################
FROM alpine:latest

WORKDIR /app

# ca-certificates for outbound HTTPS, tzdata for UTC handling, wget for healthcheck
RUN apk --no-cache add ca-certificates tzdata wget

# Create a non-root user
RUN addgroup -g 1001 -S appgroup && \
        adduser -u 1001 -S appuser -G appgroup && \
        mkdir -p /app/data/workspaces && \
        chown -R appuser:appgroup /app

COPY --from=backend --chown=appuser:appgroup /out/server /app/server
COPY --from=backend --chown=appuser:appgroup /out/worker /app/worker

USER appuser

ENV HOST=0.0.0.0 PORT=8787 WORKSPACE_DIR=/app/data/workspaces
EXPOSE 8787

# Healthcheck against the /health endpoint (internal/router)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
        CMD wget --no-verbose --tries=1 --spider http://localhost:8787/health || exit 1

CMD ["./server"]
