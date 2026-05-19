# Build the manager binary
FROM golang:1.26.2 as builder

WORKDIR /workspace

COPY argocd-operator /workspace/argocd-operator

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache dependencies
RUN go mod download

# Copy the Go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY controllers/ controllers/
COPY common/ common/
COPY version/ version/

# Build explicitly for linux/amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager ./cmd/main.go

# Use distroless as minimal base image
FROM --platform=linux/amd64 gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager /usr/local/bin/manager

# Install redis artifacts
COPY build/redis /var/lib/redis

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/manager"]