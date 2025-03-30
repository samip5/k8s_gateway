FROM alpine:3.21 AS certs

RUN apk --update --no-cache add ca-certificates

FROM golang:alpine

COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY coredns /

EXPOSE 53 53/udp
CMD ["/coredns"]
