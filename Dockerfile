# Build binaries with goreleaser first (make build/docker); the web UI is
# embedded in the binary, so the image needs nothing else.
FROM --platform=$BUILDPLATFORM busybox:1.38.0@sha256:a46257e06c82ad85242d0d38077d0a52d9896389ca60a1a59d3463db586e5fa4 AS build
ARG TARGETOS
ARG TARGETARCH
COPY dist/basket_${TARGETOS}_${TARGETARCH}/basket /basket

RUN mkdir -p /data/backups /tmp && chmod 755 /data /data/backups && chmod 1777 /tmp

FROM ghcr.io/zekihan/distroless:1.0.0-r1@sha256:af9aab3cd99d1f40bca0b21ec21fe4da082422a5bc148b152c35a45802c69a2a AS final
COPY --from=build --chown=25120:25120 /data /data
COPY --from=build --chmod=1777 /tmp /tmp
COPY --from=build /basket /usr/local/bin/basket
COPY LICENSE /usr/share/licenses/basket/LICENSE
USER 25120:25120
EXPOSE 8080 9090
VOLUME ["/data"]
ENV DB_PATH=/data/basket.db BACKUP_DIR=/data/backups LISTEN_ADDR=:8080
ENTRYPOINT ["basket"]
LABEL org.opencontainers.image.documentation="https://github.com/zekihan/basket#readme" \
      org.opencontainers.image.source="https://github.com/zekihan/basket" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.title="basket" \
      org.opencontainers.image.description="Personal shopping list and wishlist"
