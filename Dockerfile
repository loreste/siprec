# Multi-stage Docker build for SIPREC server
# Stage 1: Build environment with all dependencies
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    make \
    gcc \
    g++ \
    musl-dev \
    cmake

# Build bcg729 from source for G.729 codec support
RUN git clone https://gitlab.linphone.org/BC/public/bcg729.git /tmp/bcg729 && \
    cd /tmp/bcg729 && \
    cmake -DCMAKE_INSTALL_PREFIX=/usr -DENABLE_STATIC=YES -DENABLE_SHARED=YES . && \
    make -j$(nproc) && \
    make install && \
    rm -rf /tmp/bcg729

# Set working directory
WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application with optimizations (CGO enabled for bcg729 G.729 codec)
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags='-w -s' \
    -o siprec \
    ./cmd/siprec

# Build test environment binary
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags='-w -s' \
    -o testenv \
    ./cmd/testenv

# Stage 2: Test runner (for running tests in container)
FROM builder AS tester

# Install test dependencies
RUN apk add --no-cache curl jq

# Copy test scripts and data
COPY test/ ./test/
COPY test-recordings/ ./test-recordings/

# Run tests (this stage can be used for CI/CD, CGO enabled for bcg729)
RUN CGO_ENABLED=1 go test -v ./... -race -coverprofile=coverage.out

# Stage 3: Final production image
FROM alpine:edge AS production

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    jq \
    libstdc++ \
    libgcc

# Copy bcg729 shared library from builder
COPY --from=builder /usr/lib/libbcg729.so* /usr/lib/

# Create non-root user for security
RUN addgroup -g 1000 siprec && \
    adduser -D -s /bin/sh -u 1000 -G siprec siprec

# Create necessary directories
RUN mkdir -p /app/recordings /app/sessions /app/logs /app/certs && \
    chown -R siprec:siprec /app

# Set working directory
WORKDIR /app

# Copy compiled binaries from builder stage
COPY --from=builder --chown=siprec:siprec /build/siprec /app/
COPY --from=builder --chown=siprec:siprec /build/testenv /app/

# Copy configuration files and scripts
COPY --chown=siprec:siprec scripts/docker-entrypoint.sh ./entrypoint.sh
COPY --chown=siprec:siprec run.sh ./

# Make scripts executable
RUN chmod +x /app/siprec /app/testenv /app/run.sh /app/entrypoint.sh

# Switch to non-root user
USER siprec

# Expose ports
EXPOSE 8080 5060/udp 5060/tcp

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Set entrypoint
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command
CMD ["siprec"]

# Stage 4: Development image with additional tools
FROM builder AS development

# Install development tools
RUN apk add --no-cache \
    curl \
    jq \
    vim \
    bash \
    htop

# Install Go tools for development
RUN go install golang.org/x/tools/cmd/goimports@latest && \
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    go install github.com/swaggo/swag/cmd/swag@latest

# Set working directory
WORKDIR /workspace

# Copy everything for development
COPY . .

# Make all scripts executable
RUN find . -name "*.sh" -exec chmod +x {} \;

# Expose ports for development (including debug port)
EXPOSE 8080 5060/udp 5060/tcp 2345

# Development command (keeps container running)
CMD ["tail", "-f", "/dev/null"]