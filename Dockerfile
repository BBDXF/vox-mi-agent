# syntax=docker/dockerfile:1
# Binaries are cross-compiled on the host (CGO_ENABLED=0) into dist/linux_${TARGETARCH}/;
# this Dockerfile only assembles images. See Makefile.
# certs/extra-ca.crt (empty in git; local corporate proxy CA) is appended to the CA bundle in build-only stages.
# The runtime stage never contains it.

ARG ALPINE_VERSION=3.22
ARG GO_VERSION=1.27

# ---- tools: pinned fzq / jq / yq, SHA256-verified (tools.lock) ----
FROM alpine:${ALPINE_VERSION} AS tools
ARG TARGETARCH
COPY certs/extra-ca.crt /tmp/extra-ca.crt
RUN if [ -s /tmp/extra-ca.crt ]; then cat /tmp/extra-ca.crt >> /etc/ssl/certs/ca-certificates.crt; fi
RUN apk add --no-cache curl
RUN if [ -s /tmp/extra-ca.crt ]; then cat /tmp/extra-ca.crt >> /etc/ssl/certs/ca-certificates.crt; fi
COPY tools.lock /src/tools.lock
COPY scripts/fetch-tools.sh /src/scripts/fetch-tools.sh
RUN LOCK=/src/tools.lock sh /src/scripts/fetch-tools.sh /out "${TARGETARCH}"

# ---- test: Go toolchain + tools, runs `go test` against mounted source ----
FROM golang:${GO_VERSION}-alpine AS test
COPY certs/extra-ca.crt /tmp/extra-ca.crt
RUN apk add --no-cache ca-certificates curl bash coreutils findutils tzdata
RUN if [ -s /tmp/extra-ca.crt ]; then cat /tmp/extra-ca.crt >> /etc/ssl/certs/ca-certificates.crt; fi
COPY --from=tools /out/ /usr/local/bin/
ENV VOX_TOOLS_DIR=/usr/local/bin CGO_ENABLED=0
WORKDIR /src
CMD ["go", "test", "./..."]

# ---- runtime ----
FROM alpine:${ALPINE_VERSION} AS runtime
ARG TARGETARCH
RUN apk add --no-cache ca-certificates curl tzdata bash coreutils findutils libstdc++ libgcc \
 && addgroup -g 1000 app && adduser -D -u 1000 -G app app \
 && mkdir -p /data /tmp/toolrun && chown -R app:app /data /tmp/toolrun
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
