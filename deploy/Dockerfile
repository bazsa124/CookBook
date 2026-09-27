# Optional container build for Linux hosts that prefer Docker.
# Build from the repo root:  docker build -f deploy/Dockerfile -t cookbook .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/cookbook ./cmd/cookbook \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/cookbook-import ./cmd/cookbook-import

FROM alpine:3.22
ARG TYPST_VERSION=0.14.0
RUN apk add --no-cache ca-certificates tzdata xz ffmpeg \
 && arch="$(uname -m)" \
 && wget -qO- "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-${arch}-unknown-linux-musl.tar.xz" \
    | tar -xJ -C /tmp \
 && mv /tmp/typst-*/typst /usr/local/bin/typst \
 && rm -rf /tmp/typst-* \
 && adduser -D -H -u 10001 cookbook
COPY --from=build /out/ /usr/local/bin/
USER cookbook
VOLUME /data
ENV COOKBOOK_DATA=/data \
    # Inside a container the Tailscale interface is not visible; publish the
    # port on the host's Tailscale IP instead (see docker-compose.yml).
    COOKBOOK_LISTEN=0.0.0.0:8738
EXPOSE 8738
ENTRYPOINT ["/usr/local/bin/cookbook"]
