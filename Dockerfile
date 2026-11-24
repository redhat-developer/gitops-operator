# Build the manager binary
FROM golang:1.27.3 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# ... and the go-replaced module
COPY argocd-operator/ argocd-operator/
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
# Retry failed dependency downloads with HTTP/2 disabled.
RUN go mod download && exit 0; \
    for retry in 1 2 3 4 5; do \
        echo "go mod download failed. Retrying in 30 seconds (retry ${retry}/5)."; \
        sleep 30 || exit $?; \
        GODEBUG=http2client=0 go mod download && exit 0; \
        status=$?; \
    done; \
    exit "$status"

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY controllers/ controllers/
COPY common/ common/
COPY internal/ai/ internal/ai/
COPY version/ version/

# Build - Use TARGETARCH to build for the correct architecture
ARG TARGETARCH
ARG VERSION=0.0.1
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a \
    -ldflags="-X github.com/redhat-developer/gitops-operator/version.Version=${VERSION}" \
    -o manager ./cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager /usr/local/bin/manager

# install redis artifacts
COPY build/redis /var/lib/redis

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/manager"]
