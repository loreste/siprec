# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -o siprec-server .

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/siprec-server .

# Create directories
RUN mkdir -p /app/recordings /app/sessions /app/certs

# Copy sample environment file (will be overridden by volume)
COPY .env.example /app/.env

# Expose ports
EXPOSE 5060/udp 5060/tcp 5061/udp 5061/tcp 5062/tcp 8080/tcp
EXPOSE 10000-20000/udp

# Set volume for recordings, sessions and configuration
VOLUME ["/app/recordings", "/app/certs", "/app/sessions"]

CMD ["./siprec-server"]