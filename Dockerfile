# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /solarshowdown-api

# Final stage
FROM alpine:latest
ARG TZ

WORKDIR /

# Copy the binary from builder
COPY --from=builder /solarshowdown-api /solarshowdown-api

# Install tzdata and set timezone
RUN apk add --no-cache tzdata && \
    ln -snf /usr/share/zoneinfo/${TZ} /etc/localtime && \
    echo ${TZ} > /etc/timezone

# Expose the port the server listens on
EXPOSE 8080

# Run the application
CMD ["/solarshowdown-api"]
