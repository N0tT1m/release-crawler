# Multi-stage build for smallest possible image
FROM golang:1.24-alpine AS builder

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o azure-search-server azure-search-server.go

# Final stage - minimal image
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create app directory
WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/azure-search-server .

# Expose port
EXPOSE 8080

# Set environment variables with defaults
ENV PORT=8080
ENV AZURE_SEARCH_SERVICE=""
ENV AZURE_SEARCH_KEY=""
ENV AZURE_SEARCH_INDEX="talkdesk-docs"

# Run the application
CMD ["./azure-search-server"]