FROM --platform=${BUILDPLATFORM} docker.io/library/golang:1.24 as builder

ARG LDFLAGS
ARG VERSION=dev
ARG REVISION=dev

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

FROM debian:stable-slim

RUN apt-get update && apt-get -uy upgrade
RUN apt-get -y install ca-certificates && update-ca-certificates

FROM scratch

COPY --from=0 /etc/ssl/certs /etc/ssl/certs
COPY --from=builder /build/coredns .

EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
