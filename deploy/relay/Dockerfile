# Build stage compiles the server and its health probe as static binaries.
FROM golang:1.27-alpine3.23 AS app-build

WORKDIR /build

RUN apk --no-cache add upx

COPY go.mod go.sum ./
RUN go mod download

COPY . /build

# The version is reported by the binary and by the metrics.
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
        -o unison-relay ./cmd/unison-relay && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" \
        -o healthcheck ./deploy/relay/healthcheck && \
    upx -9 --lzma unison-relay healthcheck

# The runtime image holds the two binaries and nothing else: no shell, no package
# manager, no writable paths.
FROM scratch

WORKDIR /app

COPY --from=app-build /build/unison-relay /app/unison-relay
COPY --from=app-build /build/healthcheck /app/healthcheck

# Production shape: QUIC and the WebSocket fallback share one TLS port, health and
# metrics keep a port of their own.
ENV UNISON_RELAY_QUIC_ADDR=":443"
ENV UNISON_RELAY_WS_ADDR=":443"
ENV UNISON_RELAY_HTTP_ADDR=":8080"

USER 10001:10001

EXPOSE 443/udp
EXPOSE 443/tcp
EXPOSE 8080/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/healthcheck"]

ENTRYPOINT ["/app/unison-relay"]
