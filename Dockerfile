
ARG VERSION=dev
ARG REVISION=dev
ARG TARGETPLATFORM=linux/amd64
FROM docker.io/library/golang:1.24-alpine AS builder

WORKDIR /build

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

COPY cmd/ cmd/
COPY *.go ./

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0 \
    GO111MODULE=on \
    GOARCH=$TARGETARCH \
    GOOS=$TARGETOS

RUN go build -ldflags "-s -w -X github.com/coredns/coredns/coremain.GitCommit=${REVISION} -X main.pluginVersion=${VERSION}" -o coredns cmd/coredns.go

# Update CA Certs
FROM docker.io/library/alpine:3.22 AS certs

RUN apk --update --no-cache add ca-certificates

# Final Build
FROM scratch

COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=builder /build/coredns .

EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
