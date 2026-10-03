# The binary is built by GitHub Actions and copied next to this file.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY tainavpn-server /usr/local/bin/tainavpn-server
VOLUME /data
ENTRYPOINT ["/usr/local/bin/tainavpn-server"]
