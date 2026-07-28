# Build stage
FROM golang:1.18-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo \
    -ldflags='-w -s -extldflags "-static"' \
    -o fpl-bot .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/fpl-bot .

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD pgrep -f fpl-bot || exit 1

ENTRYPOINT ["/app/fpl-bot"]
