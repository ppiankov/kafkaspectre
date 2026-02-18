# syntax=docker/dockerfile:1
ARG GO_VERSION=1.22.0

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source
COPY . .

# Build the kafkaspectre binary
# Use CGO_ENABLED=0 to create a statically linked binary
# Use -a -installsuffix cgo to ensure all packages are rebuilt from source without CGO
# Embed version information using LDFLAGS
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" -o /bin/kafkaspectre ./cmd/kafkaspectre

FROM alpine/git AS git_cloner
WORKDIR /temp
# Install git to clone the repo inside the build context to get git information
RUN apk add --no-cache git

FROM gcr.io/distroless/static-debian12 AS final

# Copy the built binary from the builder stage
COPY --from=builder /bin/kafkaspectre /usr/local/bin/kafkaspectre

# Expose any necessary ports (if applicable for future features)
# EXPOSE 9092

# Set the entrypoint to the kafkaspectre binary
ENTRYPOINT ["kafkaspectre"]