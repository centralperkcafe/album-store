FROM golang:1.23-alpine

WORKDIR /app

# Install required build tools
RUN apk add --no-cache gcc musl-dev

# Copy go.mod, go.sum and main.go (copy go.sum too for better caching)
COPY go.mod go.sum main.go ./

# Download dependencies (Go 1.16+ automatically uses the vendor directory if present)
RUN go mod download
# Optional: Verify dependencies, go mod tidy might not be strictly necessary if go.sum is up-to-date
# RUN go mod tidy
# RUN go list -m all

# Copy project source code
COPY . .

# Build application
# Use CGO_ENABLED=0 for a static binary if no CGo is needed
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o album-service .

# Expose port
EXPOSE 8080

# Run application
CMD ["./album-service"]
