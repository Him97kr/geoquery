# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Download dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/geoquery ./server/main.go

# ── Run stage ─────────────────────────────────────────────────────────────────
FROM alpine:latest

# Add CA certificates for HTTPS API calls
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/geoquery .

EXPOSE 8080

CMD ["./geoquery"]
