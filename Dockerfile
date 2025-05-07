# Build stage
FROM golang:1.24.2-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/pac-quota-controller ./cmd/pac-quota-controller

# Final stage
FROM alpine:3.21.3

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/bin/pac-quota-controller .

# Set environment variables
ENV PAC_QUOTA_CONTROLLER_PORT=8080 \
    PAC_QUOTA_CONTROLLER_LOG_LEVEL=info \
    PAC_QUOTA_CONTROLLER_ENV=production

# Expose port
EXPOSE 8080

# Run the application
CMD ["./pac-quota-controller"]
