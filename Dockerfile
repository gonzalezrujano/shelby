# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Install git and required dependencies
RUN apk add --no-cache git

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/shelby ./cmd/shelby

# Final stage
FROM alpine:latest

WORKDIR /app

# Install generic useful packages like ca-certificates and tzdata
RUN apk add --no-cache ca-certificates tzdata

# Set an environment variable for the store directory, and make it a volume
ENV SHELBY_HOME=/data
VOLUME /data

# Expose web UI port
EXPOSE 8080

# Copy the binary from the build stage
COPY --from=builder /app/bin/shelby /usr/local/bin/shelby

ENTRYPOINT ["shelby"]
