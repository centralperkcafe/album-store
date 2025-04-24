FROM golang:1.23-alpine
WORKDIR /app

# Install required build tools
RUN apk add --no-cache gcc musl-dev

# Copy go.mod, go.sum and Go files (copy go.sum for caching)
COPY go.mod go.sum ./
COPY *.go ./

# Download dependencies
RUN go mod download
# Optional: Verify or tidy
# RUN go mod tidy

# Build the application
# Use CGO_ENABLED=0 for a static binary if no CGo is needed
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o inventory-service .

# Expose port
EXPOSE 8081

# Run the application
CMD ["./inventory-service"]
