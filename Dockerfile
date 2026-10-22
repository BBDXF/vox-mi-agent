# syntax=docker/dockerfile:1
# Binaries are cross-compiled on the host (CGO_ENABLED=0) into dist/linux_${TARGETARCH}/;
# this Dockerfile only assembles images. See Makefile.
# Optional local-only build secret "extraca" (corporate proxy CA); CI builds without it.

ARG ALPINE_VERSION=3.22
ARG GO_VERSION=1.27

# ---- tools: pinned fzq / jq / yq, SHA256-verified (tools.lock) ----
FROM alpine:${ALPINE_VERSION} AS tools
ARG TARGETARCH
RUN --mount=type=secret,id=extraca,required=false \
    if [ -s /run/secrets/extraca ]; then cat /run/secrets/extraca >> /etc/ssl/certs/ca-certificates.crt; fi
RUN apk add --no-cache curl
COPY tools.lock /src/tools.lock
COPY scripts/fetch-tools.sh /src/scripts/fetch-tools.sh
RUN LOCK=/src/tools.lock sh /src/scripts/fetch-tools.sh /out "${TARGETARCH}"

# ---- test: Go toolchain + tools, runs `go test` against mounted source ----
FROM golang:${GO_VERSION}-alpine AS test
RUN --mount=type=secret,id=extraca,required=false \
    if [ -s /run/secrets/extraca ]; then cat /run/secrets/extraca >> /etc/ssl/certs/ca-certificates.crt; fi
RUN apk add --no-cache ca-certificates curl bash coreutils findutils tzdata
COPY --from=tools /out/ /usr/local/bin/
ENV VOX_TOOLS_DIR=/usr/local/bin CGO_ENABLED=0
WORKDIR /src
CMD ["go", "test", "./..."]

# ---- runtime ----
FROM alpine:${ALPINE_VERSION} AS runtime
ARG TARGETARCH
RUN --mount=type=secret,id=extraca,required=false \
    if [ -s /run/secrets/extraca ]; then cat /run/secrets/extraca >> /etc/ssl/certs/ca-certificates.crt; fi
RUN apk add --no-cache ca-certificates curl tzdata bash coreutils findutils \
 && addgroup -g 1000 app && adduser -D -u 1000 -G app app \
 && mkdir -p /data /tmp/toolrun && chown -R app:app /data /tmp/toolrun
# apk's ca-certificates trigger rebuilds the bundle: re-append the local-only CA.
RUN --mount=type=secret,id=extraca,required=false \
    if [ -s /run/secrets/extraca ]; then cat /run/secrets/extraca >> /etc/ssl/certs/ca-certificates.crt; fi
COPY --from=tools /out/ /usr/local/bin/
COPY dist/linux_${TARGETARCH}/ /usr/local/bin/
ENV VOX_TOOLS_DIR=/usr/local/bin VOX_DATA_DIR=/data
USER app
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD ["app", "healthcheck"]
ENTRYPOINT ["app"]
CMD ["serve"]
